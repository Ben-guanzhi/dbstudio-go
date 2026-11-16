package main

// Connection store: the saved connections live in a JSON file under the
// app's user-data directory, so they come back on the next launch. Nothing
// here touches the UI or the database.
//
// Passwords do not sit in the JSON in the clear: save seals them through the
// keyring beside the file ("enc:v1:..." values), load opens them back, so every
// caller — the connection form, OpenDB — keeps handling a plain Conn as it
// always did. A password an old file stored in the clear passes through load
// unchanged and is sealed the first time the file is saved again; no migration
// step is asked of the user.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"dbstudio-go/internal/keyring"
)

// Conn is one saved connection. Only the fields meaningful for Kind are used.
type Conn struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"`
	Database string `json:"database,omitempty"`
	Schema   string `json:"schema,omitempty"`
	File     string `json:"file,omitempty"`
	SSLMode  string `json:"sslMode,omitempty"`

	// The SSH bastion (empty SSHHost = no tunnel). The tunnel carries every
	// pooled connection, so a database on a private network is reached the way
	// a person would reach it. SSHPassword is a secret: the store seals it
	// exactly as it seals Password.
	SSHHost       string `json:"sshHost,omitempty"`
	SSHPort       int    `json:"sshPort,omitempty"`
	SSHUser       string `json:"sshUser,omitempty"`
	SSHPassword   string `json:"sshPassword,omitempty"`
	SSHKeyFile    string `json:"sshKeyFile,omitempty"`
	SSHPassphrase string `json:"sshPassphrase,omitempty"`
	// SSHKnownHosts 是连接指定的可信主机密钥文件（空则用系统默认的
	// known_hosts）。备份/恢复走隧道时用它确认对端，而不是静默信任。
	SSHKnownHosts string `json:"sshKnownHosts,omitempty"`

	// ReadOnly locks the connection against statements that change data or
	// schema. It is what a production connection should carry, and it is saved
	// with the connection because a lock that resets each launch is not a lock.
	ReadOnly bool `json:"readOnly,omitempty"`
	// Color is a palette key ("red", "amber", …) the navigator and the status
	// bar show: the point of a colour is to see at a glance which server this
	// window is pointed at before typing "DELETE FROM".
	Color string `json:"color,omitempty"`
}

// HistoryEntry is one statement that ran, kept for the history modal. The
// elapsed time is stored as the string the status line showed, so the history
// reads the same the day it ran and later.
type HistoryEntry struct {
	SQL      string    `json:"sql"`
	OK       bool      `json:"ok"`
	Elapsed  string    `json:"elapsed"`
	At       time.Time `json:"at"`
	Favorite bool      `json:"favorite,omitempty"`
}

// histMax is how many statements the history keeps: older ones are dropped as
// newer ones run, so the file stays bounded no matter how long the app is used.
const histMax = 200

// Store is the connection list, kept in a file.
type Store struct {
	mu       sync.Mutex
	path     string
	histPath string
	conns    []Conn
	active   string
	hist     []HistoryEntry
	keys     *keyring.Keyring
}

// NewStore returns a store backed by connections.json in the app's user data
// directory, falling back to the working directory where that is unavailable.
func NewStore() (*Store, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil || dir == "" {
		dir = "."
	}
	return NewStoreAt(filepath.Join(dir, "connections.json"))
}

// NewStoreAt returns a store backed by the file at path. The history lives in
// a file of its own beside it, and the keyring in keyring.bin beside that.
func NewStoreAt(path string) (*Store, error) {
	hist := strings.TrimSuffix(path, filepath.Ext(path)) + ".history.json"
	dir := filepath.Dir(path)
	return &Store{
		path:     path,
		histPath: hist,
		keys:     keyring.Open(filepath.Join(dir, "keyring.bin")),
	}, nil
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var f struct {
		Conns  []Conn `json:"connections"`
		Active string `json:"active"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return err
	}
	// Passwords come back as they were saved: sealed values are opened here, so
	// the rest of the app handles a plain Conn. A value that fails to open is
	// dropped rather than carried as garbage; the connection can be re-entered,
	// and a tampered file must not yield a plausible password.
	for i := range f.Conns {
		f.Conns[i].Password = s.openSecret(f.Conns[i].Password)
		f.Conns[i].SSHPassword = s.openSecret(f.Conns[i].SSHPassword)
		f.Conns[i].SSHPassphrase = s.openSecret(f.Conns[i].SSHPassphrase)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conns = f.Conns
	s.active = f.Active
	return nil
}

// loadHistory reads the history file, leaving the history empty when it is
// absent or unreadable: a missing history is not an error worth showing.
func (s *Store) loadHistory() {
	data, err := os.ReadFile(s.histPath)
	if err != nil {
		return
	}
	var hist []HistoryEntry
	if err := json.Unmarshal(data, &hist); err != nil {
		return
	}
	s.mu.Lock()
	s.hist = hist
	s.mu.Unlock()
}

// sealSecret seals one stored secret for the file, or answers "" when it cannot:
// a connection that cannot be sealed is written without the secret rather than
// with it in the clear.
func (s *Store) sealSecret(plain string) string {
	if plain == "" {
		return ""
	}
	sealed, err := s.keys.Encrypt(plain)
	if err != nil {
		return ""
	}
	return sealed
}

// openSecret opens one stored secret. A value that cannot be opened (a tampered
// file, another keyring's ciphertext) becomes "" rather than carrying garbage
// into a connect attempt; a cleartext value from an old file passes through.
func (s *Store) openSecret(value string) string {
	if value == "" {
		return ""
	}
	plain, err := s.keys.Decrypt(value)
	if err != nil {
		return ""
	}
	return plain
}

// save writes the file. The caller must hold s.mu; Add, Update and Remove do.
//
// The file's copy of each connection seals its password through the keyring
// first: what lands on disk is "enc:v1:...", never the secret. The in-memory
// list keeps the plaintext, so a save does not disturb the callers working with
// a Conn. A password that fails to seal is stored empty rather than in the
// clear — an unusable connection beats a leaked one.
func (s *Store) save() error {
	f := struct {
		Conns  []Conn `json:"connections"`
		Active string `json:"active"`
	}{append([]Conn(nil), s.conns...), s.active}
	for i := range f.Conns {
		f.Conns[i].Password = s.sealSecret(f.Conns[i].Password)
		f.Conns[i].SSHPassword = s.sealSecret(f.Conns[i].SSHPassword)
		f.Conns[i].SSHPassphrase = s.sealSecret(f.Conns[i].SSHPassphrase)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}

// saveHistory writes the history file. The caller must hold s.mu.
func (s *Store) saveHistory() error {
	if err := os.MkdirAll(filepath.Dir(s.histPath), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(append([]HistoryEntry(nil), s.hist...), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.histPath, data, 0o600)
}

// AddHistory prepends a statement that ran and writes the file. The store keeps
// its own lock, so a run's goroutine can record without the UI asking for it.
func (s *Store) AddHistory(e HistoryEntry) {
	if strings.TrimSpace(e.SQL) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hist = append([]HistoryEntry{e}, s.hist...)
	if len(s.hist) > histMax {
		s.hist = s.hist[:histMax]
	}
	// A history that cannot be written is not worth stopping a run for, but it
	// should not be silent either: the caller's status is left alone.
	_ = s.saveHistory()
}

// ListHistory returns the history, newest first.
func (s *Store) ListHistory() []HistoryEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]HistoryEntry(nil), s.hist...)
}

// ToggleHistoryFavorite flips the favorite mark on the i-th entry and writes
// the file. A favorite is a statement the user marked to find again — the
// history keeps everything that ran, and a favorite is the one they want back
// without scrolling.
func (s *Store) ToggleHistoryFavorite(i int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if i < 0 || i >= len(s.hist) {
		return false
	}
	s.hist[i].Favorite = !s.hist[i].Favorite
	_ = s.saveHistory()
	return s.hist[i].Favorite
}

// ClearHistory drops every entry and writes the empty file.
func (s *Store) ClearHistory() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hist = nil
	return s.saveHistory()
}

// List returns the connections in file order.
func (s *Store) List() []Conn {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Conn(nil), s.conns...)
}

// Get finds a connection by name.
func (s *Store) Get(name string) (Conn, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.conns {
		if c.Name == name {
			return c, true
		}
	}
	return Conn{}, false
}

// Add appends a connection and makes it active.
func (s *Store) Add(c Conn) error {
	normalize(&c)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.conns {
		if e.Name == c.Name {
			return errors.New("a connection with that name already exists")
		}
	}
	s.conns = append(s.conns, c)
	s.active = c.Name
	return s.save()
}

// Update replaces the connection of that name.
func (s *Store) Update(c Conn) error {
	normalize(&c)
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, e := range s.conns {
		if e.Name == c.Name {
			s.conns[i] = c
			s.active = c.Name
			return s.save()
		}
	}
	return errors.New("connection not found")
}

// Upsert adds the connection, or replaces it when its name is taken.
func (s *Store) Upsert(c Conn) error {
	if _, ok := s.Get(c.Name); ok {
		return s.Update(c)
	}
	return s.Add(c)
}

// Dir is the directory the connections and exports live in.
func (s *Store) Dir() string { return filepath.Dir(s.path) }

// Remove deletes the connection of that name.
func (s *Store) Remove(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.conns[:0]
	for _, c := range s.conns {
		if c.Name != name {
			out = append(out, c)
		}
	}
	s.conns = out
	if s.active == name {
		if len(s.conns) > 0 {
			s.active = s.conns[0].Name
		} else {
			s.active = ""
		}
	}
	return s.save()
}

// Active returns the active connection.
func (s *Store) Active() (Conn, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.conns {
		if c.Name == s.active {
			return c, true
		}
	}
	if len(s.conns) > 0 {
		return s.conns[0], true
	}
	return Conn{}, false
}

// kindsAvailable lists the known engine kinds, in display order: the built-in
// drivers plus whatever plugin executables discovery registered (波 100 续：
// 随 Discover/Undiscover 即时变化，表单每次打开都读新值).
func kindsAvailable() []string {
	out := append([]string{"sqlite", "mysql"}, PluginKinds()...)
	sort.Strings(out[2:])
	return out
}

func defaultPort(kind string) int {
	switch kind {
	case "mysql":
		return 3306
	case "postgres":
		return 5432
	case "mssql":
		return 1433
	case "oracle":
		return 1521
	case "redis":
		return 6379
	}
	return 0
}

// hostDefaults fills in the conventional host and port for a new connection.
func hostDefaults(c *Conn) {
	if c.Kind == "sqlite" {
		c.Host, c.Port = "", 0
		return
	}
	if c.Host == "" {
		c.Host = "127.0.0.1"
	}
	if c.Port == 0 {
		c.Port = defaultPort(c.Kind)
	}
	if c.Kind == "postgres" && c.Schema == "" {
		c.Schema = "public"
	}
	if c.Kind == "mssql" && c.Schema == "" {
		c.Schema = "dbo"
	}
}

// label renders a connection for lists and the window title.
func (c Conn) label() string {
	switch c.Kind {
	case "sqlite":
		if c.File == "" || c.File == ":memory:" {
			return c.Name + " (in-memory)"
		}
		return c.Name + " (" + c.File + ")"
	case "redis":
		return c.Name + " (" + c.Host + ":" + itoa(c.Port) + ")"
	default:
		return c.Name + " (" + c.User + "@" + c.Host + ":" + itoa(c.Port) + "/" + c.Database + ")"
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// normalize trims a connection's fields.
func normalize(c *Conn) {
	c.Name = strings.TrimSpace(c.Name)
	c.Kind = strings.ToLower(strings.TrimSpace(c.Kind))
	c.Host = strings.TrimSpace(c.Host)
	c.User = strings.TrimSpace(c.User)
	c.Database = strings.TrimSpace(c.Database)
	c.Schema = strings.TrimSpace(c.Schema)
	c.File = strings.TrimSpace(c.File)
	c.SSLMode = strings.ToLower(strings.TrimSpace(c.SSLMode))
}
