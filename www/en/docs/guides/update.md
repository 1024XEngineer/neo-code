# Updating

## Automatic update check

NeoCode silently checks for a newer version in the background at startup (3-second timeout). The update notice is printed after you exit the TUI, so it does not interrupt the session.

## Manual upgrade

Upgrade to the latest stable release:

```bash
neocode update
```

Include pre-release versions:

```bash
neocode update --prerelease
```

## Version info

- Release builds have a version injected via `ldflags`
- Local development builds report `dev`
