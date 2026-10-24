<!-- arc:begin -->
## Architecture (arc)

The architecture registry is `.arch/registry.yaml`; `ARCHITECTURE.md` has the rendered view.

- Before feature work, run `arc brief <paths>` (or `arc brief` for everything) and follow the owning concept's rules.
- Extend the concept that owns the code; do not build a parallel version of something that exists.
- New concepts, tables, env vars and flags need a registry entry with a reason in the same change.
- Replacing something means deleting the old code in the same change, or adding a `removals` entry.
- Before creating files in a new directory, run `arc place <path>`; unowned files fail `arc gate`.
- After editing the registry, run `arc render` and `arc validate`.
- When filing a bead, include `Replaces / removes:`, `Concepts touched:` (registry concept ids, comma-separated) and `Closest existing thing:` lines; `arc triage` checks them.
<!-- arc:end -->
