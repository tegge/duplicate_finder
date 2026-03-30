# Security Policy

## Supported versions

| Version | Supported |
|---------|-----------|
| latest `main` | ✅ |

## Reporting a vulnerability

Please **do not** open a public GitHub issue for security vulnerabilities.

Instead, report them via GitHub's private vulnerability reporting:  
**Security → Report a vulnerability** on the repository page.

Include:
- A description of the vulnerability
- Steps to reproduce
- Potential impact (e.g. unintended file deletion, path traversal)

You will receive a response within 7 days.

## Security considerations for this tool

- **Default dry-run**: no files are modified unless `-delete` or `-trash` is explicitly passed.
- **No network access**: the tool reads only local files and writes local output files.
- **Path traversal**: all paths are resolved with `filepath.Abs` + `filepath.Clean` before use; `isUnder` uses a separator-aware prefix check to prevent `/../` escapes.
- **Origin protection**: files under `-origin` are never placed in the delete list regardless of other flags.
