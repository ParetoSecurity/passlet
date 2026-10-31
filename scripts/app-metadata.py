#!/usr/bin/env python3
"""Build signing metadata from the supplied Apple provisioning profile."""
import datetime
import os
from pathlib import Path
import plistlib
import sys

profile_path, output, version, mode = sys.argv[1:]
if mode not in ('development', 'release', 'unsigned'):
    raise SystemExit('Invalid build mode')
if mode == 'unsigned':
    identifier = 'dev.passlet.preview'
    prefix = ''
else:
    profile = plistlib.loads(Path(profile_path).read_bytes())
    ent = profile['Entitlements']
    identifier = ent.get('com.apple.application-identifier', ent.get('application-identifier'))
    prefix = profile['ApplicationIdentifierPrefix'][0]
    if not identifier or '*' in identifier or not identifier.startswith(prefix + '.'):
        raise SystemExit('An explicit App ID provisioning profile is required')
    if profile['ExpirationDate'] <= datetime.datetime.now(datetime.timezone.utc).replace(tzinfo=None):
        raise SystemExit('Provisioning profile has expired')
    if mode == 'release':
        if ent.get('get-task-allow') or profile.get('ProvisionedDevices'):
            raise SystemExit('Release requires a Developer ID distribution profile, not a development profile')
        if identifier.endswith('.keychain-prototype'):
            raise SystemExit('Release requires a production App ID, not the prototype identity')
app = Path(output)
(app / 'Contents/MacOS').mkdir(parents=True, exist_ok=True)
(app / 'Contents/Resources').mkdir(parents=True, exist_ok=True)
if mode != 'unsigned':
    entitlements = {
        'com.apple.application-identifier': identifier,
        'com.apple.developer.team-identifier': profile['TeamIdentifier'][0],
        'keychain-access-groups': [identifier],
    }
    (app.parent / 'entitlements.plist').write_bytes(plistlib.dumps(entitlements))
info = {
    'CFBundleIdentifier': identifier if mode == 'unsigned' else identifier[len(prefix) + 1:],
    'CFBundleName': 'Passlet', 'CFBundleDisplayName': 'Passlet',
    'CFBundleExecutable': 'PassletHelper', 'CFBundlePackageType': 'APPL',
    'CFBundleVersion': version, 'CFBundleShortVersionString': version,
    'CFBundleIconFile': 'Passlet', 'LSMinimumSystemVersion': '14.0', 'LSUIElement': True,
}
(app / 'Contents/Info.plist').write_bytes(plistlib.dumps(info))
# The optional menu bar app is nested and has no restricted entitlements.
menu = app / 'Contents/Helpers/PassletMenu.app/Contents'
(menu / 'MacOS').mkdir(parents=True, exist_ok=True)
(menu / 'Resources').mkdir(parents=True, exist_ok=True)
menu_info = dict(info, CFBundleIdentifier=info['CFBundleIdentifier'] + '.menu',
                 CFBundleName='Passlet Menu', CFBundleDisplayName='Passlet Menu', CFBundleExecutable='PassletMenu')
(menu / 'Info.plist').write_bytes(plistlib.dumps(menu_info))
