# Sanho Configuration

Load this reference only when selecting built-in workspace configuration or
hook integration. Sanho does not provide workflow authoring.

## Built-in workspace configuration

Initialization accepts these supported choices:

```bash
sanho init \
  --project <project> \
  --docs-repo-url <url> \
  --docs-dir <repository-relative-directory> \
  --actor-email <email>
```

The project and repository URL are required inputs; never guess them. The docs
directory defaults to `docs`, and the actor email defaults to Git's user email.
Use defaults unless the user or repository authority establishes another
value. Do not edit `.sanho.json` manually.

A project registration can be created independently with explicit intent:

```bash
sanho project add <project> --docs-repo-url <url>
```

Inspect registered configuration with `sanho state --all --json`. The registry
is observational and is not authority for a workspace's docs derivation.

## Custom hooks and Husky

For a repository-local custom `core.hooksPath` or recognized Husky 9 layout,
initialization requires explicit opt-in:

```bash
sanho init \
  --project <project> \
  --docs-repo-url <url> \
  --manage-custom-hooks
```

The same flag is available on `sanho migrate`. Do not use it for global,
repository-external, symlinked, or unrecognized hook paths. Sanho preserves
foreign hook content and manages only recognized lines; never edit those lines
by hand. Verify the configured target with `sanho doctor --json` after setup.

## No custom workflow authoring

Sanho has no selectable workflow engine and no custom policy, manifest,
procedure, session, or task definition format. Its behavior is the built-in
Git and docs synchronization contract. Preserve that product boundary instead
of creating configuration files or inventing CLI commands.
