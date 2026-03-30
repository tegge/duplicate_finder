## Summary

<!-- What does this PR change and why? -->

## Type of change

- [ ] Bug fix
- [ ] New feature
- [ ] Refactor / cleanup
- [ ] Documentation
- [ ] CI / tooling

## Checklist

- [ ] `make all` passes (tidy → fmt → vet → test → build)
- [ ] New flags have sensible defaults that preserve existing behaviour
- [ ] Dry-run safety is preserved (no files modified without `-delete` or `-trash`)
- [ ] At least one copy per hash group is always kept
- [ ] `-origin` protected paths are never in the delete list
- [ ] CHANGELOG.md updated under `[Unreleased]`

## Testing

<!-- Describe how you tested the change. Include any relevant test file paths or commands. -->
