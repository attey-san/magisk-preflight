package main

import (
	"os"
	"path/filepath"
)

// writeFixture writes a full module tree under testdata/<name>/ from a
// path->contents map. Existing files are left in place.
func writeFixture(root string, files map[string]string) error {
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			return err
		}
	}
	return nil
}

// goodProp is the smallest module.prop that should never be flagged.
const goodProp = `id=cleanmodule
name=Clean Module
version=v1.2.3
versionCode=42
author=someone
description=Does nothing risky, used as the clean baseline in tests.
`

const goodUpdater = "#MAGISK\n"

// cleanModule is a realistic small module that must lint without findings.
var cleanModule = map[string]string{
	"module.prop": goodProp,
	"META-INF/com/google/android/updater-script": goodUpdater,
	"README.md": `# cleanmodule

Replaces /system/etc/hosts with an ad-blocking list.

Booting Android safe mode disables this module and restores stock hosts;
use that to recover if DNS breaks after flashing.
`,
	"customize.sh": `ui_print "- Extracting module files"
unzip -o "$ZIPFILE" "system/*" -d "$MODPATH" >/dev/null
set_perm_recursive "$MODPATH" 0 0 0755 0644
`,
	"post-fs-data.sh": `#!/system/bin/sh
MODDIR=${0%/*}
mount -o bind "$MODDIR/system/etc/hosts" /system/etc/hosts
`,
	"service.sh": `#!/system/bin/sh
MODDIR=${0%/*}
while [ "$(getprop sys.boot_completed)" != "1" ]; do
    sleep 5
done
"$MODDIR/scripts/refresh.sh" &
`,
	"scripts/refresh.sh": `#!/system/bin/sh
. "$MODDIR/config"
echo ok
`,
	"config":           "REFRESH=60\n",
	"system/etc/hosts": "127.0.0.1 localhost\n",
}

// Each entry is one rule's minimal failing case: the fixture adds these files
// on top of a valid base so exactly the target rule fires.
var brokenByRule = map[string]map[string]string{
	"meta": {
		"META-INF/com/google/android/updater-script": "# nothing\n",
	},
	"prop": {
		"module.prop": "id=9bad\nname=X\nversion=1\nauthor=a\ndescription=d\ngarbage line\n",
	},
	"crlf": {
		"service.sh":      "#!/system/bin/sh\r\necho hi\r\n",
		"post-fs-data.sh": "#!/system/bin/sh\n",
	},
	"shebang": {
		"service.sh": "#!/bin/bash\necho hi\n",
	},
	"bashism": {
		"service.sh": "#!/system/bin/sh\nif [[ -f /data/x ]]; then source /data/x; fi\narr=(a b)\nfunction f { echo $'x'; }\n",
	},
	"su": {
		"service.sh": "#!/system/bin/sh\nsu -c 'id'\n",
	},
	"postfsdata": {
		"post-fs-data.sh": "#!/system/bin/sh\nsleep 5\ncurl https://example.com/a.zip\n",
	},
	"partition": {
		"service.sh": "#!/system/bin/sh\nmount -o rw,remount /system\necho x > /system/build.prop.tmp && mv /system/build.prop.tmp /system/build.prop\ndd if=/data/img of=/vendor/img\nrm -rf /product/overlay\n",
	},
	"vendordir": {
		"vendor/etc/x.conf": "key=value\n",
	},
	"iptablesw": {
		"service.sh": "#!/system/bin/sh\niptables -w 5 -A OUTPUT -j DROP\n",
	},
	"safemode": {
		"README.md":       "# x\n\nNo recovery instructions of any kind appear below this line.\n",
		"post-fs-data.sh": "#!/system/bin/sh\nsleep 5\n",
	},
	"tls": {
		"module.prop": goodProp + "minApi=24\n",
		"service.sh":  "#!/system/bin/sh\nwget https://example.com/data.json\n",
	},
}
