import pathlib,json,subprocess,hashlib,sys,datetime
D=pathlib.Path(__file__).parent;M=json.loads((D/'manifest.json').read_text())
def call(cap):
 r=subprocess.run(['/private/tmp/jev-benchmark-router-fixed','call',cap,'--config','/private/tmp/jev-expanded-2026-10-15/startup-private.json','--json','-'],input='{}',text=True,capture_output=True,check=True);r=json.loads(r.stdout);assert r['ok'];return r['data']
assert call('status.get')['version']==11
for cap in ['prompt.get','decision.get']:assert call(cap)==json.loads((D/'frozen'/f'{cap}.json').read_text()),cap
now=call('models.list');old=json.loads((D/'frozen/models.list.json').read_text());fields=['id','provider_id','upstream_name','description','location','enabled','capabilities'];assert [{k:r[k] for k in fields} for r in now['items']]==[{k:r[k] for k in fields} for r in old['items']]
for p,h in json.loads((D/'freeze-hashes.json').read_text()).items():assert hashlib.sha256((D/p).read_bytes()).hexdigest()==h,p
out={'at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'jev_version':11,'model_descriptions_prompt_decision_config_unchanged':True,'frozen_files_unchanged':True,'SR':'Frozen archived configuration and per-task choices reused offline; SR runtime is not invoked'}
if '--preflight' not in sys.argv:
 P=json.loads((D/'prompts.json').read_text());selection=json.loads((D/'attempt-selection.json').read_text());assert len(selection)==768;records={r['request_id']:r for r in json.loads((D/'routing-records.json').read_text())};n=0
 for p in (D/'attempts').glob('*.json'):
  if p.name.endswith('.started.json'):continue
  r=json.loads(p.read_text());j=r['job'];started=json.loads(p.with_name(p.stem+'.started.json').read_text())['request'];assert started=={'model':M['models'][j['model']],'messages':[{'role':'user','content':P[j['id']]}],**M['generation']}
  if r['status']==200:
   assert r['response']['model']==M['models'][j['model']];rec=records[r['request_id']];assert rec['mode']=='explicit' and rec['decision_usage']['invocations']==0 and rec['config_version']==11
  n+=1
 assert len({(r['job']['id'],r['job']['model'],r['job']['replicate']) for r in selection})==768
 for p in (D/'scores').glob('*/*-container.json'):
  s=json.loads(p.read_text());assert s['ExitCode']==0 and not s['OOMKilled']
 out.update(attempts=n,selected_samples=768,explicit_model_calls_only=True,generation_parameters_identical=True)
(D/('preflight-validation.json' if '--preflight' in sys.argv else 'validation.json')).write_text(json.dumps(out,indent=2));print(json.dumps(out))
