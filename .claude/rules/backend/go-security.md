---
paths: ["services/go-api/**/*.go"]
---

# Go security

### Security thinking model

Before writing or reviewing code, ask three questions:

1. **What are the trust boundaries?** -- Where does untrusted data enter the system? (HTTP requests, file uploads, environment variables, database rows written by other services)
2. **What can an attacker control?** -- Which inputs flow into sensitive operations? (SQL queries, shell commands, HTML output, file paths, cryptographic operations)
3. **What is the blast radius?** -- If this defense fails, what's the worst outcome? (Data leak, RCE, privilege escalation, denial of service)

### Severity levels

| Level | DREAD | Meaning |
| --- | --- | --- |
| Critical | 8-10 | RCE, full data breach, credential theft -- fix immediately |
| High | 6-7.9 | Auth bypass, significant data exposure, broken crypto -- fix in current sprint |
| Medium | 4-5.9 | Limited exposure, session issues, defense weakening -- fix in next sprint |
| Low | 1-3.9 | Minor info disclosure, best-practice deviations -- fix opportunistically |

### Quick reference

| Severity | Vulnerability | Defense | Standard Library Solution |
| --- | --- | --- | --- |
| Critical | SQL Injection | Parameterized queries separate data from code | `database/sql` with `?` placeholders |
| Critical | Command Injection | Pass args separately, never via shell concatenation | `exec.Command` with separate args |
| High | XSS | Auto-escaping renders user data as text, not HTML/JS | `html/template`, `text/template` |
| High | Path Traversal | Scope untrusted file access to an allowed root | Go 1.24+: use `os.Root`. Pre-1.24: `filepath.IsLocal` + `filepath.Rel` |
| Medium | Timing Attacks | Constant-time comparison avoids byte-by-byte leaks | `crypto/subtle.ConstantTimeCompare` |
| High | Crypto Issues | Use vetted algorithms; never roll your own | `crypto/aes`, `crypto/rand` |
| Medium | HTTP Security | TLS + security headers prevent downgrade attacks | `net/http`, configure TLSConfig |
| Low | Missing Headers | HSTS, CSP, X-Frame-Options prevent browser attacks | Security headers middleware |
| Medium | Rate Limiting | Rate limits prevent brute-force and resource exhaustion | `golang.org/x/time/rate`, server timeouts |
| High | Race Conditions | Protect shared state to prevent data corruption | `sync.Mutex`, channels, avoid shared state |

### Threat modeling (STRIDE)

Apply STRIDE to every trust boundary crossing and data flow: **S**poofing (authentication), **T**ampering (integrity), **R**epudiation (audit logging), **I**nformation Disclosure (encryption), **D**enial of Service (rate limiting), **E**levation of Privilege (authorization). Score each threat using DREAD to prioritize remediation.

### Research before reporting

Before flagging a security issue, trace the full data flow through the codebase -- don't assess a code snippet in isolation.

1. **Trace the data origin** -- follow the variable back to where it enters the system
2. **Check for upstream validation** -- look for input validation, sanitization, type parsing, or allow-listing earlier in the call chain
3. **Examine the trust boundary** -- if the data never crosses a trust boundary, the risk profile is different
4. **Read the surrounding code, not just the diff** -- middleware, interceptors, or wrapper functions may already provide defense

When downgrading or skipping a finding: add a brief inline comment (e.g., `// security: SQL concat safe here -- input is validated by parseUserID() which returns int`) so the decision is documented.

### Tooling & verification

```bash
# Go security checker (SAST)
go tool gosec ./...

# Vulnerability scanner
go tool govulncheck ./...

# Race detector
go test -race ./...

# Fuzz testing
go test -fuzz=Fuzz
```

### Common mistakes

| Severity | Mistake | Fix |
| --- | --- | --- |
| High | `math/rand` for tokens | Output is predictable. Use `crypto/rand` |
| Critical | SQL string concatenation | Attacker can modify query logic. Use parameterized queries |
| Critical | `exec.Command("bash -c")` | Shell interprets metacharacters. Pass args separately |
| High | Trusting unsanitized input | Validate at trust boundaries |
| Critical | Hardcoded secrets | Secrets in source code end up in version history. Use env vars or secret managers |
| Medium | Comparing secrets with `==` | `==` short-circuits on first differing byte. Use `crypto/subtle.ConstantTimeCompare` |
| Medium | Returning detailed errors | Stack traces help attackers. Return generic messages, log details server-side |
| High | Ignoring `-race` findings | Races cause data corruption and can bypass authorization. Fix all races |
| High | MD5/SHA1 for passwords | Known collision attacks, fast to brute-force. Use Argon2id or bcrypt |
| High | AES without GCM | ECB/CBC lack authentication. GCM provides encrypt+authenticate |
| Medium | Binding to 0.0.0.0 | Exposes to all interfaces. Bind to specific interface |

### Security anti-patterns

| Severity | Anti-Pattern | Why It Fails | Fix |
| --- | --- | --- | --- |
| High | Security through obscurity | Hidden URLs are discoverable via fuzzing, logs, or source | Authentication + authorization on all endpoints |
| High | Trusting client headers | `X-Forwarded-For`, `X-Is-Admin` are trivially forged | Server-side identity verification |
| High | Client-side authorization | JavaScript checks are bypassed by any HTTP client | Server-side permission checks on every handler |
| High | Shared secrets across envs | Staging breach compromises production | Per-environment secrets via secret manager |
| Critical | Ignoring crypto errors | `_, _ = encrypt(data)` silently proceeds unencrypted | Always check errors -- fail closed, never open |
| Critical | Rolling your own crypto | Custom encryption hasn't been analyzed by cryptographers | Use `crypto/aes` GCM, `golang.org/x/crypto/argon2` |
