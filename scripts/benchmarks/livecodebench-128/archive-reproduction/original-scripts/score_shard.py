import pathlib,json,sys
from lcb_runner.benchmarks.code_generation import CodeGenerationProblem
from lcb_runner.evaluation.compute_code_generation_metrics import codegen_metrics
from lcb_runner.utils.extraction_utils import extract_code
from lcb_runner.lm_styles import LMStyle
from lcb_runner.evaluation import testing_util
original_guard=testing_util.reliability_guard
def guard(maximum_memory_bytes=None):
 baseline=int(next(x.split()[1] for x in pathlib.Path('/proc/self/status').read_text().splitlines() if x.startswith('VmSize:')))*1024
 return original_guard(maximum_memory_bytes=baseline+2*1024**3)
testing_util.reliability_guard=guard
D=pathlib.Path('/experiment');model=sys.argv[1];start=int(sys.argv[2]);problems=[]
with pathlib.Path('/source/selected.jsonl').open() as f:
 for i,line in enumerate(f):
  if start<=i<start+8:problems.append(CodeGenerationProblem(**json.loads(line)))
  if i+1>=start+8:break
ids=['LCB/'+p.platform.value+'/'+p.question_id for p in problems];selection={(r['job']['id'],r['job']['model'],r['job']['replicate']):r['selected'] for r in json.loads((D/'attempt-selection.json').read_text())};codes=[]
for t in ids:
 row=[]
 for rep in [1,2,3]:
  r=json.loads((D/selection[t,model,rep]).read_text());content=''.join(c.get('text','') for c in r.get('response',{}).get('content',[]) if c.get('type')=='text');row.append(extract_code(content,LMStyle.OpenAIChat) if r['status']==200 else '')
 codes.append(row)
result=codegen_metrics([p.get_evaluation_sample() for p in problems],codes,k_list=[1],num_process_evaluate=1,timeout=6)
out={'model':model,'start':start,'ids':ids,'replicates':[1,2,3],'codes':codes,'results':result[1],'metadata':result[2]}
pathlib.Path('/out',f'{start:03}.json').write_text(json.dumps(out,indent=2,default=lambda x:x.item() if hasattr(x,'item') else str(x)));print('SHARD_COMPLETE',flush=True)
