package sessionscope

// Origin distinguishes a password-authenticated login session from a minted token:
//   - Login sessions can mint new tokens and revoke any of the user's other sessions.
//   - Minted tokens are dead-ends — they act per their Scope but can never touch another
//     session (can't mint further tokens, can't revoke a session other than themselves).
const (
	OriginLogin  = "login"
	OriginMinted = "minted"
)

// Scope gates trading: only Write may place or cancel orders. A login session is always Write.
const (
	ScopeRead  = "read"
	ScopeWrite = "write"
)
