package main

// Why a section is not there (gap-analysis follow-up: the explanation, not just
// the hiding).
//
// The tree hides an object section the driver answers ErrNotSupported for — the
// kernel's answer is the honest one ("this engine has no such concept"), but it
// is phrased for a caller, not for a person: "ERR_NOT_SUPPORTED: operation not
// supported for this engine" does not say which engine, or which concept. So
// when a section resolves unsupported, the status line says it in words:
//
//	SQLite has no standalone sequences, so the section is hidden.
//
// The wording is the client's (it names the concept the user just clicked), and
// the engine's name is the one the connection kind maps to — the same spelling
// the connection form and the export filters use. Nothing here claims a version
// or a per-version feature: an engine that gains a concept will answer the read,
// and the section simply appears.

// engineLabel is the name users know for a connection kind.
func engineLabel(kind string) string {
	switch kind {
	case "sqlite":
		return "SQLite"
	case "mysql":
		return "MySQL"
	case "postgres", "postgresql":
		return "PostgreSQL"
	case "mssql", "sqlserver":
		return "SQL Server"
	case "oracle":
		return "Oracle"
	case "dm":
		return "DM"
	case "redis":
		return "Redis"
	}
	if kind == "" {
		return "This engine"
	}
	return kind
}

// conceptOf is what a section's objects are called in a sentence.
func conceptOf(kind int) string {
	switch kind {
	case objSequence:
		return "standalone sequences"
	case objRoutine:
		return "stored functions or procedures"
	case objEnum:
		return "user-defined enum types"
	case objPartition:
		return "declarative table partitioning"
	case objCheck:
		return "a CHECK-constraint catalog"
	case objTrigger:
		return "a trigger catalog"
	case objIndex:
		return "an index catalog"
	case objFK:
		return "a foreign-key catalog"
	}
	return "this object type"
}

// unsupportedNote is the sentence the status line shows when a section turns out
// to be a concept the engine does not have.
func unsupportedNote(sec objectSection, engine string) string {
	return engineLabel(engine) + " has no " + conceptOf(sec.kind) + ", so the section is hidden"
}
