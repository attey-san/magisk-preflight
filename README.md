# preflight

Static analysis for Magisk modules, run on your desktop instead of your phone.

Every existing tool in this space is reactive: on-device bootloop protectors
that wait two minutes after a failed boot, then disable every installed module
and reboot. That is useless when the module wrote to /system directly, and it
never tells you *what* was wrong. preflight reads a module directory or zip
and tells you what will break before you flash it. It never touches a device.

```
$ preflight lint ~/build/my-module
error    module.prop: missing required key "versionCode"; magisk rejects the module at install
error    post-fs-data.sh:1: bash does not exist at this path on android; the script dies with 'no such file or directory' when run directly
error    post-fs-data.sh:6: sleep in post-fs-data blocks the entire boot; move it to service.sh which runs non-blocking after late_start
warning  module does risky work but has no README documenting that booting android safe mode disables it
```

## Installation

With Go installed:

```
go install github.com/attey-san/magisk-preflight@latest
```

Or build from a checkout:

```
git clone https://github.com/attey-san/magisk-preflight && cd magisk-preflight
go build -o preflight .
```

No cgo, so it cross-compiles for the device and runs under Termux:

```
GOOS=android GOARCH=arm64 go build -o preflight .
```

## Commands

`preflight new <name>` scaffolds a correct module skeleton: valid module.prop,
LF endings, script stubs using `MODDIR=${0%/*}`, the META-INF layout with
`#MAGISK` in updater-script.

`preflight lint <path>` analyses a module directory or zip. Findings are
human-readable by default, JSON with `--json`. Exit status is 1 when any
error-severity finding is present, so CI can gate on it.

`preflight simulate <path>` resolves what the module would actually do:
which target paths its system/ tree replaces or merges into (honouring
`.replace` and `.skip_mount`) and which scripts run in which stage, in order.
An opaque zip becomes a plan you can read before trusting it with a reboot:

```
$ preflight simulate my-module.zip
system overlay:
  /bin           merge
      busybox
scripts, in run order:
  customize.sh     install time (as root, on flash)
  post-fs-data.sh  post-fs-data (blocking, pre-zygote)  [blocks boot]
  service.sh       late_start service (non-blocking)

$ preflight simulate replace-etc.zip
system overlay:
  /etc           replace
      hosts
      (every stock file in /etc not listed above is hidden)
```

A `.replace` marker applies to the directory holding it, not to that
directory's parent, so a module can swap one app and keep merging everything
beside it:

```
$ preflight simulate one-app.zip
system overlay:
  /app           merge
      Bar/Bar.apk
  /app/Foo       replace
      Foo.apk
      (every stock file in /app/Foo not listed above is hidden)
```

`.skip_mount` follows the same rule.

Flags work in any position (`preflight lint mod --json` is the same as
`preflight lint --json mod`).

## The ruleset

Structural:

- Module files must sit at the zip root. Nesting everything one folder deep
  installs fine and then does nothing; it is the most common packaging mistake.
- `META-INF/com/google/android/updater-script` must contain `#MAGISK`, and
  `update-binary` must exist: Magisk executes it to install anything.
- module.prop needs id, name, version, versionCode, author, description. The
  id must match `^[a-zA-Z][a-zA-Z0-9._-]+$`, versionCode must be an integer,
  and a newline inside description breaks Magisk's parser.
- CRLF line endings anywhere in module.prop or shell scripts. Android's sh
  chokes on the trailing `\r` and the failure looks nothing like its cause.
- Symlinks are reported rather than silently followed; an absolute target
  resolves against the phone's filesystem and replaces whatever lives there.

Boot-stage correctness:

- post-fs-data.sh runs blocking, before Zygote. `sleep`, unbounded loops,
  `wait`, or any network call there stalls every boot and can bootloop.
- There is no network in post-fs-data. curl/wget/ping at that stage always fail.
- Work that belongs in service.sh (late_start, non-blocking) sitting in
  post-fs-data instead.

Shell portability — Android ships mksh at /system/bin/sh, not bash:

- `#!/bin/bash` or `#!/usr/bin/env bash` shebangs. That path does not exist.
- Bashisms: `[[ ]]`, `==` inside `[ ]`, arrays, `function`, `source`, `$'...'`,
  process substitution, herestrings, `${var/pat/repl}`.
- Calling `su` from inside a module script: you are already root, and it can
  deadlock.

Systemless integrity:

- Remounting a partition read-write, or writing to /system, /vendor or
  /product outside the module's own overlay. This defeats systemless design,
  survives uninstall, and bricks devices where the partition is immutable or
  under dm-verity. Highest severity in the tool.
- A bare top-level `vendor/` directory instead of `system/vendor/`.
- `.replace` semantics: the marker makes Magisk swap the directory holding it
  rather than merge into it, silently hiding every stock file that directory
  had. simulate always reports this, at the directory it actually applies to.

Legacy-device traps:

- `iptables -w 5` takes no argument on older kernels; plain `-w` works.
- Android 7.0 (API 24) and earlier have no ISRG Root X1 in the trust store, so
  https fetches to Let's Encrypt hosts fail with a bare certificate error.
  Flagged when module.prop's minApi or customize.sh indicates API <= 24.
- No safe-mode escape documented. Booting Android safe mode disables all
  modules; a module doing anything risky should say so in its README.

Rules live in one registry, one struct per rule with its own test, so adding
the next one is a few lines.
