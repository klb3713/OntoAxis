#!/usr/bin/env bash
# Checks that every secretKeyRef used by rendered charts maps to an existing
# target Secret key produced by ESO, and every kind ExternalSecret remoteRef
# property exists in the bootstrap Secret. Read-only; requires a live kind
# cluster with ESO-synced secrets.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
for c in platform data app observability jobs; do
  helm template t "$ROOT/deploy/helm/ontoaxis-$c" -f "$ROOT/deploy/values/kind.yaml" > "$TMP/$c.yaml"
done
cat "$TMP"/*.yaml > "$TMP/all.yaml"
python3 - "$TMP/all.yaml" "$ROOT/deploy/argocd/secrets/kind/10-externalsecrets.yaml" <<'PY'
import json,re,subprocess,sys
all_yaml=open(sys.argv[1]).read()
es_yaml=open(sys.argv[2]).read()
refs=[]
for m in re.finditer(r'secretKeyRef:\s*\n\s*name:\s*([^\s]+)\s*\n\s*key:\s*([^\s]+)',all_yaml):
    refs.append((m.group(1),m.group(2)))
p=subprocess.run(['kubectl','get','secrets','-A','-o','json'],capture_output=True,text=True)
if p.returncode!=0:
    print('ERROR: cannot list secrets:',p.stderr); sys.exit(1)
secrets={}
for item in json.loads(p.stdout).get('items',[]):
    secrets.setdefault(item['metadata']['name'],{}).update(item.get('data',{}))
missing=[]
for name,key in sorted(set(refs)):
    data=secrets.get(name)
    if data is None:
        missing.append(f'{name}/{key}: Secret missing'); continue
    if key not in data:
        missing.append(f'{name}/{key}: key missing')
props=re.findall(r'property:\s*([a-z0-9-]+)',es_yaml)
p=subprocess.run(['kubectl','-n','external-secrets','get','secret','ontoaxis-bootstrap','-o','json'],capture_output=True,text=True)
if p.returncode!=0:
    missing.append('bootstrap Secret missing')
else:
    data=json.loads(p.stdout).get('data',{})
    for prop in sorted(set(props)):
        if prop not in data: missing.append(f'bootstrap/{prop}: property missing')
print(f'secretKeyRef entries: {len(set(refs))}; ESO properties: {len(set(props))}')
if missing:
    print('MISSING:'); [print(' -',x) for x in missing]; sys.exit(1)
print('OK: chart secret refs and ESO bootstrap properties are complete')
PY
