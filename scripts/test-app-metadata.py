#!/usr/bin/env python3
"""Ensure release metadata cannot accidentally accept development credentials."""
import datetime
from pathlib import Path
import plistlib
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).with_name('app-metadata.py')

class MetadataTests(unittest.TestCase):
    def run_profile(self, changes=None, mode='release'):
        profile = {
            'ApplicationIdentifierPrefix': ['TEAM'], 'TeamIdentifier': ['TEAM'],
            'ExpirationDate': datetime.datetime(2099, 1, 1),
            'Entitlements': {'com.apple.application-identifier': 'TEAM.dev.passlet.app'},
        }
        profile.update(changes or {})
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            path = root / 'profile.plist'
            path.write_bytes(plistlib.dumps(profile))
            result = subprocess.run(['python3', str(SCRIPT), str(path), str(root / 'Passlet.app'), '0.1.0', mode], capture_output=True)
            metadata = None
            if result.returncode == 0:
                metadata = plistlib.loads((root / 'entitlements.plist').read_bytes())
                self.menu_info = plistlib.loads((root / 'Passlet.app/Contents/Helpers/PassletMenu.app/Contents/Info.plist').read_bytes())
            return result.returncode, metadata

    def test_distribution_entitlements(self):
        code, entitlements = self.run_profile()
        self.assertEqual(code, 0)
        self.assertEqual(entitlements['keychain-access-groups'], ['TEAM.dev.passlet.app'])
        self.assertNotIn('get-task-allow', entitlements)

    def test_menu_app_metadata(self):
        self.assertEqual(self.run_profile()[0], 0)
        self.assertEqual(self.menu_info['CFBundleIdentifier'], 'dev.passlet.app.menu')
        self.assertEqual(self.menu_info['CFBundleExecutable'], 'PassletMenu')
        self.assertEqual(self.menu_info['CFBundleShortVersionString'], '0.1.0')
        self.assertTrue(self.menu_info['LSUIElement'])

    def test_release_rejects_device_bound_profile(self):
        self.assertNotEqual(self.run_profile({'ProvisionedDevices': ['device']})[0], 0)

    def test_release_rejects_debug_entitlement(self):
        self.assertNotEqual(self.run_profile({'Entitlements': {'com.apple.application-identifier': 'TEAM.dev.passlet.app', 'get-task-allow': True}})[0], 0)

    def test_release_rejects_prototype(self):
        self.assertNotEqual(self.run_profile({'Entitlements': {'com.apple.application-identifier': 'TEAM.dev.passlet.keychain-prototype'}})[0], 0)

    def test_rejects_expired_and_wildcard_profiles(self):
        self.assertNotEqual(self.run_profile({'ExpirationDate': datetime.datetime(2000, 1, 1)})[0], 0)
        self.assertNotEqual(self.run_profile({'Entitlements': {'com.apple.application-identifier': 'TEAM.*'}})[0], 0)

if __name__ == '__main__':
    unittest.main()
