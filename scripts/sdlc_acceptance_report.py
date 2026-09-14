#!/usr/bin/env python3
"""Derive functional coverage and outcome measures from retained executed evidence."""
import hashlib
import json
import sys
from pathlib import Path

root = Path(sys.argv[1])
exit_code = int(sys.argv[2])
image = sys.argv[3]
events = []
for line in (root / 'tests.jsonl').read_text().splitlines():
    if line.startswith('{'):
        events.append(json.loads(line))
results = {e['Package'].split('hybrid-ai-platform/')[-1] + ':' + e['Test']: e['Action']
           for e in events if e.get('Test') and e.get('Action') in ('pass', 'fail', 'skip')}
coverage = json.loads(Path('tests/sdlc-coverage.json').read_text())
items = []
for item in coverage['items']:
    checks = list(item['checks'])
    if item.get('local_model_check') and item['local_model_check'] in results:
        checks.append(item['local_model_check'])
    items.append({'id': item['id'], 'checks': {c: results.get(c, 'missing') for c in checks},
                  'status': 'pass' if all(results.get(c) == 'pass' for c in checks) else 'fail'})
measurements = {}
for path in root.glob('TestSDLC*/*/execution.json'):
    view = json.loads(path.read_text())
    run = view['run']
    models = [s['result']['model'] for s in view['steps'] if (s.get('result') or {}).get('model')]
    kind = 'local_model' if models and all(m['trial_kind'] == 'local_model' for m in models) else 'fixture'
    measurements[run['id']] = {'trial_kind': kind, 'kind': run['kind'], 'status': run['status'],
        'usage': run['usage'], 'duration_seconds': None,
        'model_duration_millis': sum(m['duration_millis'] for m in models),
        'model_identities': sorted({m['model'] + '@' + m['model_sha256'] for m in models}),
        'failed_steps': sum((s.get('result') or {}).get('outcome') in ('failed','blocked') for s in view['steps']),
        'outcome_evidence': str(path.relative_to(root))}
report = {'schema': 'hybrid-ai/sdlc-acceptance/v1', 'kind': 'disposable_functional_acceptance',
          'status': 'pass' if exit_code == 0 and results and all(v == 'pass' for v in results.values())
                    and all(i['status'] == 'pass' for i in items) else 'fail',
          'tests': results, 'exit_code': exit_code, 'evaluator_image': image, 'coverage': items,
          'source_sha256': json.loads((root / 'source-manifest.json').read_text())['source_sha256'],
          'outcome_measurements': measurements,
          'local_model_trial': results.get('internal/e2e:TestSDLCRealLocalModelFeatureE2E', 'not_requested'),
          'deferred_acceptance': coverage['deferred_acceptance']}
(root / 'summary.json').write_text(json.dumps(report, indent=2) + '\n')
entries = {}
for path in sorted(root.rglob('*')):
    if path.is_file() and path.relative_to(root).parts[0] not in ('source','bin') and path.name != 'evidence-sha256.json':
        entries[str(path.relative_to(root))] = hashlib.sha256(path.read_bytes()).hexdigest()
(root / 'evidence-sha256.json').write_text(json.dumps(entries, indent=2) + '\n')
print('SDLC functional acceptance:', report['status'], sum(i['status']=='pass' for i in items),
      '/ 12 scope items,', len(results), 'test/subtest results; local model:', report['local_model_trial'])
raise SystemExit(report['status'] != 'pass')
