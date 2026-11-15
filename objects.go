package main

// The extended object tree (P1 #8): beyond tables and their columns, a schema
// can carry standalone objects — sequences, stored functions/procedures and
// user-defined enum types — and a table can carry indexes, foreign keys, CHECK
// constraints and partitions. These are read from the kernel's SQLDriver and
// ExtraMetadataReader, both capability assertions: a driver that cannot answer
// (SQLite has no sequences, routines or partitions) hides the section instead
// of failing the tree.
//
// The tree is lazy: opening a section starts a background load, and a section
// whose driver answers ErrNotSupported is resolved empty and hidden.

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Ben-guanzhi/dbcore"
	"github.com/Ben-guanzhi/dbcore/ddl"
	"github.com/Ben-guanzhi/dbcore/sqltext"
	"github.com/egoist/mygo/ui"
)

// Node kinds beyond the base kinds, for objects a schema or table owns. They
// name the section the node opens and the leaf the section holds.
const (
	objIndex     = 1 + iota // leaf: a table's secondary index
	objFK                   // leaf: a table's foreign key
	objCheck                // leaf: a table's CHECK constraint
	objPartition            // leaf: a table's partition
	objSequence             // leaf: a schema's sequence
	objRoutine              // leaf: a schema's function/procedure
	objEnum                 // leaf: a schema's enum type
	objTrigger              // leaf: a table's trigger
)

// tableLevelKinds is the range of kinds that hang off a table rather than a
// schema; a node's parent is "schema \x00 table" for those.
func tableLevel(kind int) bool { return kind >= objIndex && kind <= objPartition || kind == objTrigger }

// objectSection describes one skippable branch of the extended tree. A section
// is shown only after its driver has answered the read (and did not say the
// concept is unsupported); a section resolved empty is still shown, so the
// branch with no objects is honest rather than silently absent.
type objectSection struct {
	label   string // the branch's display text, e.g. "Indexes", "Functions"
	kind    int    // objIndex..objEnum, the kind of the section's leaves
	isTable bool   // true: hangs off a table (indexes/FKs/checks/partitions); false: off a schema
}

// schemaSections are the object branches a schema node can hold, in order.
var schemaSections = []objectSection{
	{label: "Sequences", kind: objSequence},
	{label: "Functions", kind: objRoutine},
	{label: "Enums", kind: objEnum},
}

// tableSections are the object branches a table node can hold, in order.
var tableSections = []objectSection{
	{label: "Indexes", kind: objIndex, isTable: true},
	{label: "Foreign Keys", kind: objFK, isTable: true},
	{label: "CHECK Constraints", kind: objCheck, isTable: true},
	{label: "Partitions", kind: objPartition, isTable: true},
	{label: "Triggers", kind: objTrigger, isTable: true},
}

// allSections is the union, for lookups by kind.
var allSections = append(append([]objectSection(nil), schemaSections...), tableSections...)

// sectionOf returns the section descriptor for an object kind.
func sectionOf(kind int) objectSection {
	for _, s := range allSections {
		if s.kind == kind {
			return s
		}
	}
	return objectSection{kind: kind}
}

// sectionTarget decodes a section node's parent into the schema (and table, for
// a table-level section) the section asks about.
func sectionTarget(n treeNode) (schema, table string) {
	if tableLevel(n.group) {
		// Table-level: parent is schema \x00 table.
		return tableOf(n.parent)
	}
	return n.parent, ""
}

// sectionUsable reports whether a section may be shown for a schema/table. A
// section never asked is shown (so its arrow can be clicked to load); one
// already known not to exist is dropped.
func (app *App) sectionUsable(schema, table string, s objectSection) bool {
	_, _, notSupported := app.objects.leaves(s.kind, schema, table)
	return !notSupported
}

// tableOf decodes a schema \x00 table pair.
func tableOf(parent string) (schema, table string) {
	parts := strings.SplitN(parent, "\x00", 2)
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}

// objectMenu is the right-click menu of an object leaf: what can be done with
// it. For now, viewing its definition — the same modal a table's DDL uses.
func (app *App) objectMenu(m *ui.Menu, n treeNode) {
	if m.Item("View Definition").Chosen() {
		app.openObjectDDL(n)
	}
}

// openObjectDDL shows an object leaf's definition in the DDL viewer.
func (app *App) openObjectDDL(n treeNode) {
	if n.kind != nodeObject {
		return
	}
	app.openDDL(n)
}

// extra reads the connection's ExtraMetadataReader, asserting it like the DDL
// viewer does. It is the capability the schema-level sections and the definition
// renders lean on.
func (app *App) extra() (dbcore.ExtraMetadataReader, bool) {
	if app.db == nil || app.db.Driver == nil {
		return nil, false
	}
	return dbcore.AsExtraMetadata(app.db.Driver)
}

// ---------------------------------------------------------------------------
// The section cache.
// ---------------------------------------------------------------------------

// objectCache holds a schema's or table's resolved object sections. The key is
// objectKey(kind, schema, table); the value is *objectLeaves when the section
// loaded (possibly empty) or true when the driver answered not-supported and
// the section must stay hidden. A missing key means "not yet asked".
type objectCache map[string]interface{}

// objectLeaves is a loaded section's leaf names.
type objectLeaves struct{ names []string }

// objectKey is the cache key of a section.
func objectKey(kind int, schema, table string) string {
	return strconv.Itoa(kind) + "\x00" + schema + "\x00" + table
}

// forget drops a section's memo, so the next open asks the driver again. The
// tree's retry uses it: a load that failed was recorded as resolved-empty (so
// it would not retry every frame), and retrying must undo that.
func (c objectCache) forget(kind int, schema, table string) {
	delete(c, objectKey(kind, schema, table))
}

// getLeaves returns a section's leaf names, and whether they are known. A nil
// bool means the driver answered not-supported (hide the section); empty means
// the concept exists but has no objects (show the section open, nothing inside
// is fine, or you can close it).
func (c objectCache) leaves(kind int, schema, table string) (names []string, loaded bool, notSupported bool) {
	if c == nil {
		return nil, false, false
	}
	v, ok := c[objectKey(kind, schema, table)]
	if !ok {
		return nil, false, false
	}
	if l, isLeaves := v.(*objectLeaves); isLeaves {
		return l.names, true, false
	}
	return nil, true, true // notSupported
}

// putLeaves records a section's leaves; a "notSupported" load records true.
func (c objectCache) putLeaves(kind int, schema, table string, names []string) {
	if c == nil {
		return
	}
	c[objectKey(kind, schema, table)] = &objectLeaves{names: names}
}

func (c objectCache) putNotSupported(kind int, schema, table string) {
	if c == nil {
		return
	}
	c[objectKey(kind, schema, table)] = true
}

// ---------------------------------------------------------------------------
// Loading a section.
// ---------------------------------------------------------------------------

// loadObjectSection starts the background read of a section's leaves. It is a
// no-op once the section is resolved (loaded or found unsupported). It shares
// loadAsync's shape with the table and column loads, so the "post, check the
// generation, refresh" rules live in one place.
func (app *App) loadObjectSection(schema, table string, sec objectSection) {
	if app.db == nil || app.db.SQL == nil {
		return
	}
	if _, loaded, _ := app.objects.leaves(sec.kind, schema, table); loaded {
		return
	}
	db := app.db
	extra, _ := app.extra()
	key := "o\x00" + strconv.Itoa(sec.kind) + "\x00" + schema + "\x00" + table
	if !app.beginLoad(key) {
		return // already on its way; the tree asks every frame
	}
	loadAsync(app, metadataTimeout, func(ctx context.Context) ([]string, error) {
		return app.readObjectSection(db, extra, sec, schema, table)
	}, func(res []string, err error) {
		app.endLoad(key)
		if app.objects == nil {
			app.objects = objectCache{}
		}
		if errors.Is(err, dbcore.ErrNotSupported) {
			// The engine has no such concept: hide the section rather than show
			// an empty stump — and say why, in the status line, so the vanished
			// branch does not read as a bug. A not-supported answer is a
			// successful read, so it clears a previous failure.
			app.objects.putNotSupported(sec.kind, schema, table)
			app.clearTreeErr()
			app.status = unsupportedNote(sec, app.engine())
			app.invalidate()
			return
		}
		if err != nil {
			// A real failure: report it in the tree (not the results pane), and
			// mark the section resolved-empty so it does not retry on every
			// frame.
			app.objects.putLeaves(sec.kind, schema, table, nil)
			app.setTreeErr(fmt.Errorf("%s: %w", sec.label, err), func() {
				app.objects.forget(sec.kind, schema, table)
				app.loadObjectSection(schema, table, sec)
			})
			app.invalidate()
			return
		}
		app.objects.putLeaves(sec.kind, schema, table, res)
		app.clearTreeErr()
		app.invalidate()
	})
}

// readObjectSection reads one section's leaf names from the driver, without
// touching the cache or UI. It is shared by the window and headless loads.
func (app *App) readObjectSection(db *DB, extra dbcore.ExtraMetadataReader, sec objectSection, schema, table string) ([]string, error) {
	var names []string
	var err error
	switch sec.kind {
	case objIndex:
		var idxs []dbcore.IndexInfo
		idxs, err = db.SQL.ListIndexes(context.Background(), schema, table)
		for _, i := range idxs {
			if i.Primary {
				continue // a primary key is not a separate object to list
			}
			names = append(names, i.Name)
		}
	case objFK:
		var fks []dbcore.ForeignKeyInfo
		fks, err = db.SQL.ListForeignKeys(context.Background(), schema, table)
		for _, f := range fks {
			names = append(names, f.Name)
		}
	case objCheck:
		if extra == nil {
			return nil, dbcore.ErrNotSupported
		}
		var cs []dbcore.CheckConstraintInfo
		cs, err = extra.CheckConstraints(context.Background(), schema, table)
		for _, c := range cs {
			names = append(names, c.Name)
		}
	case objPartition:
		if extra == nil {
			return nil, dbcore.ErrNotSupported
		}
		var ps []dbcore.PartitionInfo
		ps, err = extra.Partitions(context.Background(), schema, table)
		for _, p := range ps {
			names = append(names, p.Name)
		}
	case objTrigger:
		// Triggers are their own capability (TriggerLister): almost every engine
		// has them but no two catalogs share a shape.
		tl, ok := dbcore.AsTriggerLister(db.Driver)
		if !ok {
			return nil, dbcore.ErrNotSupported
		}
		var ts []dbcore.TriggerInfo
		ts, err = tl.Triggers(context.Background(), schema, table)
		for _, tg := range ts {
			names = append(names, tg.Name)
		}
	case objSequence:
		if extra == nil {
			return nil, dbcore.ErrNotSupported
		}
		var ss []dbcore.SequenceInfo
		ss, err = extra.Sequences(context.Background(), schema)
		for _, s := range ss {
			names = append(names, s.Name)
		}
	case objRoutine:
		if extra == nil {
			return nil, dbcore.ErrNotSupported
		}
		var rs []dbcore.RoutineInfo
		rs, err = extra.Routines(context.Background(), schema)
		for _, r := range rs {
			names = append(names, r.Name)
		}
	case objEnum:
		if extra == nil {
			return nil, dbcore.ErrNotSupported
		}
		var es []dbcore.EnumTypeInfo
		es, err = extra.EnumTypes(context.Background(), schema)
		for _, e := range es {
			names = append(names, e.Name)
		}
	}
	return names, err
}

// ---------------------------------------------------------------------------
// Composing an object's definition (View Definition).
// ---------------------------------------------------------------------------

// objectDLL builds the reconstruction of the object a tree leaf names, to show
// in the DDL viewer's read-only editor.
func (app *App) objectDDL(ctx context.Context, n treeNode) (string, error) {
	engine := app.engine()
	extra, _ := app.extra()
	switch n.group {
	case objIndex:
		return app.indexDDL(ctx, engine, n)
	case objFK:
		return app.foreignKeyDDL(ctx, engine, n)
	case objCheck:
		return app.checkDDL(ctx, engine, extra, n)
	case objPartition:
		return app.partitionDDL(ctx, engine, extra, n)
	case objTrigger:
		return app.triggerDDL(ctx, n)
	case objSequence:
		return app.sequenceDDL(ctx, engine, extra, n)
	case objRoutine:
		if extra == nil {
			return "", dbcore.ErrNotSupported
		}
		def, err := extra.RoutineDefinition(ctx, n.parent, n.label)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(def) == "" {
			return "", fmt.Errorf("function %q has no readable definition", n.label)
		}
		return def, nil
	case objEnum:
		return app.enumDDL(ctx, engine, extra, n)
	}
	return "", nil
}

// tableOf decodes the parent of a table-level object node (schema \x00 table).
func (n treeNode) objectTableOf() (schema, table string) {
	return tableOf(n.parent)
}

func (app *App) indexDDL(ctx context.Context, engine string, n treeNode) (string, error) {
	schema, table := n.objectTableOf()
	idxs, err := app.db.SQL.ListIndexes(ctx, schema, table)
	if err != nil {
		return "", err
	}
	for _, i := range idxs {
		if i.Name == n.label {
			return ddl.CreateIndex(engine, schema, table, ddl.Index{
				Name: i.Name, Unique: i.Unique, Primary: i.Primary, Columns: i.Columns,
			}), nil
		}
	}
	return "", fmt.Errorf("index %q not found", n.label)
}

func (app *App) foreignKeyDDL(ctx context.Context, engine string, n treeNode) (string, error) {
	schema, table := n.objectTableOf()
	fks, err := app.db.SQL.ListForeignKeys(ctx, schema, table)
	if err != nil {
		return "", err
	}
	for _, f := range fks {
		if f.Name == n.label {
			return ddl.AddForeignKey(engine, schema, table, ddl.ForeignKey{
				Name: f.Name, Columns: f.Columns, RefSchema: f.RefSchema,
				RefTable: f.RefTable, RefColumns: f.RefColumns,
				OnDelete: f.OnDelete, OnUpdate: f.OnUpdate,
			}), nil
		}
	}
	return "", fmt.Errorf("foreign key %q not found", n.label)
}

func (app *App) checkDDL(ctx context.Context, engine string, extra dbcore.ExtraMetadataReader, n treeNode) (string, error) {
	schema, table := n.objectTableOf()
	if extra == nil {
		return "", dbcore.ErrNotSupported
	}
	cs, err := extra.CheckConstraints(ctx, schema, table)
	if err != nil {
		return "", err
	}
	for _, c := range cs {
		if c.Name == n.label {
			obj, _ := sqltext.QualifiedName(engine, schema, table)
			q, _ := sqltext.QuoteIdent(engine, c.Name)
			return fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s CHECK (%s);\n", obj, q, c.Expression), nil
		}
	}
	return "", fmt.Errorf("constraint %q not found", n.label)
}

func (app *App) partitionDDL(ctx context.Context, engine string, extra dbcore.ExtraMetadataReader, n treeNode) (string, error) {
	schema, table := n.objectTableOf()
	if extra == nil {
		return "", dbcore.ErrNotSupported
	}
	ps, err := extra.Partitions(ctx, schema, table)
	if err != nil {
		return "", err
	}
	for _, p := range ps {
		if p.Name == n.label {
			text := "PARTITION " + p.Name
			if p.Bound != "" {
				text += " " + p.Bound
			}
			if p.Rows >= 0 {
				text += fmt.Sprintf("  -- ~%d rows", p.Rows)
			}
			return text + "\n", nil
		}
	}
	return "", fmt.Errorf("partition %q not found", n.label)
}

// triggerDDL returns the trigger's definition from the engine that keeps it.
// Triggers are the one object family behind a separate capability
// (TriggerLister), because almost every engine has them and no two catalogs
// share a shape.
func (app *App) triggerDDL(ctx context.Context, n treeNode) (string, error) {
	if app.db == nil {
		return "", dbcore.ErrNotSupported
	}
	tl, ok := dbcore.AsTriggerLister(app.db.Driver)
	if !ok {
		return "", dbcore.ErrNotSupported
	}
	schema, _ := n.objectTableOf()
	def, err := tl.TriggerDefinition(ctx, schema, n.label)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(def) == "" {
		return "", fmt.Errorf("trigger %q has no readable definition", n.label)
	}
	return def, nil
}

func (app *App) sequenceDDL(ctx context.Context, engine string, extra dbcore.ExtraMetadataReader, n treeNode) (string, error) {
	if extra == nil {
		return "", dbcore.ErrNotSupported
	}
	ss, err := extra.Sequences(ctx, n.parent)
	if err != nil {
		return "", err
	}
	for _, s := range ss {
		if s.Name == n.label {
			name, _ := sqltext.QualifiedName(engine, n.parent, s.Name)
			parts := []string{"CREATE SEQUENCE", name}
			if s.Increment != 0 {
				parts = append(parts, fmt.Sprintf("INCREMENT BY %d", s.Increment))
			}
			if s.Start != 0 {
				parts = append(parts, fmt.Sprintf("START WITH %d", s.Start))
			}
			return strings.Join(parts, " ") + ";\n", nil
		}
	}
	return "", fmt.Errorf("sequence %q not found", n.label)
}

func (app *App) enumDDL(ctx context.Context, engine string, extra dbcore.ExtraMetadataReader, n treeNode) (string, error) {
	if extra == nil {
		return "", dbcore.ErrNotSupported
	}
	es, err := extra.EnumTypes(ctx, n.parent)
	if err != nil {
		return "", err
	}
	for _, e := range es {
		if e.Name == n.label {
			name, _ := sqltext.QualifiedName(engine, n.parent, e.Name)
			labels := make([]string, len(e.Labels))
			for i, l := range e.Labels {
				labels[i] = sqltext.QuotedLiteral(engine, l)
			}
			return fmt.Sprintf("CREATE TYPE %s AS ENUM (%s);\n", name, strings.Join(labels, ", ")), nil
		}
	}
	return "", fmt.Errorf("enum %q not found", n.label)
}
