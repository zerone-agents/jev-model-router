import pathlib,json,subprocess
D=pathlib.Path(__file__).parent;B=pathlib.Path(json.loads((D/'manifest.json').read_text())['source_archive']);expected=json.loads((D/'manifest.json').read_text())['evaluator_manifest'];image=json.loads(subprocess.check_output(['docker','image','inspect','jev-livecodebench:28fef95']))[0];assert image['Id']==expected
for model in ['max','flash']:
 O=D/'scores'/model;O.mkdir(parents=True,exist_ok=True);O.chmod(0o777)
 for start in range(0,128,8):
  if (O/f'{start:03}.json').exists():
   s=json.loads((O/f'{start:03}-container.json').read_text());assert s['ExitCode']==0 and not s['OOMKilled'];continue
  name=f'jev-shared3-score-{model}-{start}'
  cmd=['docker','run','--name',name,'--network','none','--read-only','--user','65534:65534','--cap-drop','ALL','--security-opt','no-new-privileges','--pids-limit','256','--memory','4g','--cpus','2','--tmpfs','/tmp:rw,exec,size=512m','-v',str(D)+':/experiment:ro','-v',str(B/'evaluation')+':/source:ro','-v',str(O)+':/out','jev-livecodebench:28fef95','python','/experiment/score_shard.py',model,str(start)]
  with (O/f'{start:03}.log').open('x') as log:r=subprocess.run(cmd,stdout=log,stderr=subprocess.STDOUT)
  state=json.loads(subprocess.check_output(['docker','inspect',name]))[0]['State'];(O/f'{start:03}-container.json').write_text(json.dumps(state,indent=2));subprocess.run(['docker','rm',name],check=True,stdout=subprocess.DEVNULL);assert r.returncode==0 and not state['OOMKilled'],(model,start,state)
  result=json.loads((O/f'{start:03}.json').read_text());assert all(len(v)==3 and all(vv for vv in v) for v in result['results'].values());assert not any(json.loads(m).get('error_code')==-5 for row in result['metadata'] for m in row),(model,start,'TestRunnerError')
  print(model,start+8,'/128 tasks x3 scored',flush=True)
print('SCORING_COMPLETE',flush=True)
