"""Bounded synthetic bilingual decision probe, not a quality certification."""
import argparse
import json
import os
import urllib.request
import time

parser = argparse.ArgumentParser()
parser.add_argument('--requests', required=True, help='Go-generated synthetic fixtures')
parser.add_argument('--url', required=True, help='Local Laya service base URL')
parser.add_argument('--output', required=True)
args = parser.parse_args()
token = os.environ['LAYA_API_KEY']
from pathlib import Path
base=json.loads(Path(args.requests).read_text())['models-2']
tasks=[('en-simple','Translate hello into Chinese.','fast'),('zh-simple','把 hello 翻译成中文。','fast'),('en-hard','Prove that there are infinitely many primes and verify every logical step.','deep'),('zh-hard','证明素数有无穷多个，并逐步检查论证。','deep')]
results=[]
for name,task,expected in tasks:
 for reverse in (False,True):
  request=json.loads(json.dumps(base))
  candidates=[('fast','Simple translation and extraction, cheap and fast.'),('deep','Mathematical proofs and complex reasoning, expensive and slower.')]
  if reverse:candidates.reverse()
  request['state']={'preference':'Match task capability; prefer lower cost when sufficient.','messages':[{'role':'user','content':task}]}
  request['questions']['model']['criteria']={key:description for key,description in candidates}
  req=urllib.request.Request(args.url.rstrip('/')+'/v1/systemone',data=json.dumps(request).encode(),headers={'Authorization':'Bearer '+token,'Content-Type':'application/json'})
  started=time.monotonic()
  with urllib.request.urlopen(req,timeout=60) as response: result=json.load(response)
  results.append({'case':name,'reversed':reverse,'expected_model':expected,'answers':result['answers'],'seconds':round(time.monotonic()-started,3)})
Path(args.output).write_text(json.dumps(results,indent=2))
for r in results:print(r['case'],r['reversed'],r['answers'])
