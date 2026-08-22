package main

import "fmt"

// scaffold returns the files for a new module, keyed by slash-separated
// relative path. Everything is LF; scripts use #!/system/bin/sh because that
// is the only interpreter guaranteed to exist.
func scaffold(id string) map[string]string {
	return map[string]string{
		"module.prop": fmt.Sprintf(`id=%s
name=%s
version=v1.0.0
versionCode=1
author=yourname
description=A short description of what this does.
`, id, id),

		"META-INF/com/google/android/updater-script": "#MAGISK\n",

		// The official template's installer stub: the magisk app executes
		// this file to install the module. Without it the zip simply fails
		// to flash.
		"META-INF/com/google/android/update-binary": `#!/sbin/sh

#####################
# Environment Setup
#####################

umask 022

# echo before loading util_functions
ui_print() { echo "$1"; }

require_new_magisk() {
  ui_print "*******************************"
  ui_print " Please install Magisk v20.4+! "
  ui_print "*******************************"
  exit 1
}

#########################
# Load util_functions.sh
#########################

OUTFD=$2
ZIPFILE=$3

mount /data 2>/dev/null

[ -f /data/adb/magisk/util_functions.sh ] || require_new_magisk
. /data/adb/magisk/util_functions.sh
[ $MAGISK_VER_CODE -lt 20400 ] && require_new_magisk

install_module
exit 0
`,

		"customize.sh": `SKIPUNZIP=0
# Extract everything into $MODPATH. This runs at flash time as root.
unzip -o "$ZIPFILE" -d "$MODPATH" 2>/dev/null

ui_print "- Installing from $(basename "$ZIPFILE")"
set_perm_recursive "$MODPATH" 0 0 0755 0644
`,

		"post-fs-data.sh": `#!/system/bin/sh
# Runs blocking, before zygote. Keep it fast: no sleep, no network, no waits.
MODDIR=${0%/*}
`,

		"service.sh": `#!/system/bin/sh
# Runs non-blocking after late_start. Safe place for slow or networked work.
MODDIR=${0%/*}

while [ "$(getprop sys.boot_completed)" != "1" ]; do
    sleep 5
done
`,

		"README.md": fmt.Sprintf(`# %s

Describe what this changes and where. Mention that booting Android safe mode
disables all Magisk modules, so a safe-mode boot recovers the device if
something here misbehaves.
`, id),

		// A real overlay file rather than a .keep placeholder: this is what
		// magisk actually mounts, so simulate has something honest to show.
		"system/placeholder.txt": "remove me once the module replaces real files\n",
	}
}
