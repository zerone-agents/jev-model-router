import concurrent.futures,json,pathlib,urllib.request,urllib.error,time,datetime,subprocess,hashlib
D=pathlib.Path(__file__).parent;M=json.loads((D/'manifest.json').read_text());P=json.loads((D/'prompts.json').read_text());KEY=pathlib.Path('/private/tmp/jev-benchmark-RxrO56/inference.key').read_text().strip();OUT=D/'attempts';OUT.mkdir(exist_ok=True)
def save(p,v):
 with p.open('x') as f:json.dump(v,f,indent=2)
def cli(cap,args):
 r=subprocess.run(['/private/tmp/jev-benchmark-router-fixed','call',cap,'--config','/private/tmp/jev-expanded-2026-10-15/startup-private.json','--json','-'],input=json.dumps(args),text=True,capture_output=True,check=True);d=json.loads(r.stdout);assert d['ok'];return d['data']
def name(j,n):return f"{j['model']}-r{j['replicate']}-{j['id'].replace('/','_')}-a{n}"
def call(j,n):
 p=OUT/(name(j,n)+'.json');started=OUT/(name(j,n)+'.started.json')
 if p.exists():return json.loads(p.read_text())
 if started.exists():raise RuntimeError('Interrupted attempt needs reconciliation: '+name(j,n))
 body={'model':M['models'][j['model']],'messages':[{'role':'user','content':P[j['id']]}],**M['generation']};save(started,{'job':j,'attempt':n,'request':body,'at':datetime.datetime.now(datetime.timezone.utc).isoformat()});r={'job':j,'attempt':n};t=time.monotonic()
 try:
  with urllib.request.urlopen(urllib.request.Request('http://127.0.0.1:18094/v1/messages',data=json.dumps(body).encode(),headers={'Content-Type':'application/json','x-api-key':KEY,'anthropic-version':'2023-06-01'}),timeout=360) as response:r.update(status=response.status,response=json.load(response),headers=dict(response.headers),request_id=response.headers.get('request-id'))
 except urllib.error.HTTPError as e:
  r.update(status=e.code,request_id=e.headers.get('request-id'))
  try:r['response']=json.loads(e.read())
  except Exception:r['error']='unparseable_error'
 except Exception as e:r.update(status=0,error=type(e).__name__)
 r['seconds']=time.monotonic()-t;save(p,r);return r
def check():
 s=cli('status.get',{});assert s['version']==11 and not s['auto_fallback']['enabled'];return s
def phase(jobs,n):
 it=iter(jobs);done=0;counts={};check()
 with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:
  pending={pool.submit(call,j,n):j for j in [next(it,None) for _ in range(3)] if j}
  while pending:
   finished,_=concurrent.futures.wait(pending,return_when=concurrent.futures.FIRST_COMPLETED)
   for future in finished:
    j=pending.pop(future);r=future.result();done+=1;counts[str(r['status'])]=counts.get(str(r['status']),0)+1
    print(json.dumps({'phase_attempt':n,'completed':done,'total':len(jobs),'job':j,'status':r['status'],'seconds':round(r['seconds'],2),'status_counts':counts}),flush=True)
    if done%32==0:check()
    nxt=next(it,None)
    if nxt:pending[pool.submit(call,nxt,n)]=nxt
jobs=json.loads((D/'jobs.json').read_text());save(D/'status-before.json',check());phase(jobs,1)
failed=[j for j in jobs if json.loads((OUT/(name(j,1)+'.json')).read_text())['status']!=200]
if failed:check();phase(failed,2)
selection=[]
for j in jobs:
 p2=OUT/(name(j,2)+'.json');p=p2 if p2.exists() else OUT/(name(j,1)+'.json');selection.append({'job':j,'selected':str(p.relative_to(D))})
save(D/'attempt-selection.json',selection)
records=[];cursor=''
while True:
 page=cli('records.list',{'limit':200,**({'cursor':cursor} if cursor else {})});records+=page['records'];cursor=page.get('next_cursor')
 if not cursor:break
ids={json.loads(p.read_text()).get('request_id') for p in OUT.glob('*.json') if not p.name.endswith('.started.json')};save(D/'routing-records.json',[r for r in records if r['request_id'] in ids]);save(D/'status-after.json',check())
for p,h in json.loads((D/'freeze-hashes.json').read_text()).items():assert hashlib.sha256((D/p).read_bytes()).hexdigest()==h,p
print('GENERATION_COMPLETE',flush=True)
