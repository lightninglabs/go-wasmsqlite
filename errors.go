package wasmsqlite

import "errors"

var (
	ErrBridgeNotLoaded    = errors.New("wasmsqlite: sqlite bridge not loaded")
	ErrWorkerInitFailed   = errors.New("wasmsqlite: sqlite worker initialization failed")
	ErrAssetUnavailable   = errors.New("wasmsqlite: sqlite asset unavailable")
	ErrOPFSUnavailable    = errors.New("wasmsqlite: opfs unavailable")
	ErrPersistentRequired = errors.New("wasmsqlite: persistent storage required")
	ErrDuplicateOpen      = errors.New("wasmsqlite: database already open")
	ErrUnsupportedVFS     = errors.New("wasmsqlite: unsupported sqlite vfs")
	ErrNamedParameter     = errors.New("wasmsqlite: named parameter error")
	ErrProtocolMismatch   = errors.New("wasmsqlite: bridge protocol mismatch")
)
