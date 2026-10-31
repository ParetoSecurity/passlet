#!/usr/bin/env python3
"""Generate the reviewed catalog from GitHub's api.github.com OpenAPI JSON."""
import hashlib
import json
import pathlib
import sys

source = pathlib.Path(sys.argv[1]).read_bytes()
schema = json.loads(source)['components']['schemas']['app-permissions']['properties']
account = {'interaction_limits', 'email_addresses', 'followers', 'git_ssh_keys', 'gpg_keys', 'profile', 'starring'}
excluded = {'single_file': 'Requires a separately approved file-path configuration.',
            'enterprise_custom_properties_for_organizations': 'Enterprise-owned apps only.'}
repository = {'discussions', 'statuses', 'repository_projects', 'actions', 'contents', 'code_quality', 'metadata', 'dependabot_secrets', 'workflows', 'administration', 'merge_queues', 'security_events', 'environments', 'artifact_metadata', 'issues', 'checks', 'attestations', 'codespaces', 'repository_hooks', 'pages', 'packages', 'secrets', 'repository_custom_properties', 'pull_requests', 'deployments', 'secret_scanning_alerts', 'vulnerability_alerts'}
permissions = {}
for name, spec in sorted(schema.items()):
    if name in excluded:
        continue
    if name not in repository and name not in account and not name.startswith('organization_') and name not in {'members', 'custom_properties_for_organizations'}:
        raise SystemExit(f'Review scope classification for new permission: {name}')
    scope = 'account' if name in account else 'organization' if name.startswith('organization_') or name in {'members', 'custom_properties_for_organizations'} else 'repository'
    levels = ['read'] if name == 'metadata' else spec['enum']
    permissions[name] = {'scope': scope, 'levels': levels}
result = {'source': 'https://raw.githubusercontent.com/github/rest-api-description/main/descriptions/api.github.com/api.github.com.json',
          'sha256': hashlib.sha256(source).hexdigest(), 'excluded': excluded, 'permissions': permissions}
pathlib.Path('internal/passlet/github_permissions.json').write_text(json.dumps(result, indent=2) + '\n')
