import pathlib,json,collections,random,statistics,hashlib,math
D=pathlib.Path(__file__).parent;M=json.loads((D/'manifest.json').read_text());routes=json.loads((D/'routes.json').read_text());prices=json.loads((D/'prices.json').read_text())['generation'];sel=json.loads((D/'attempt-selection.json').read_text());bank={};rows=[]
for model in ['max','flash']:
 for p in sorted((D/'scores'/model).glob('*.json')):
  if p.name.endswith('-container.json'):continue
  s=json.loads(p.read_text())
  for i,t in enumerate(s['ids']):
   for rep in [1,2,3]:
    v=s['results'][str(i)][rep-1];meta=json.loads(s['metadata'][i][rep-1]);assert v and meta.get('error_code')!=-5
    bank[t,model,rep]={'pass':all(x>0 for x in v),'test_results':v,'metadata':meta,'code_sha256':hashlib.sha256(s['codes'][i][rep-1].encode()).hexdigest()}
assert len(bank)==768
def usage_cost(r):
 b=r.get('response',{});u=b.get('usage');model=b.get('model')
 if not isinstance(u,dict) or model not in prices or 'input_tokens' not in u or 'output_tokens' not in u:return None
 assert not u.get('cache_creation_input_tokens',0)
 p=prices[model];return (u['input_tokens']*p['input_per_million']+u['output_tokens']*p['output_per_million']+u.get('cache_read_input_tokens',0)*p['cache_read_per_million'])/1e6
for s in sel:
 j=s['job'];r=json.loads((D/s['selected']).read_text());v=bank[j['id'],j['model'],j['replicate']];v.update(job=j,status=r['status'],cost=usage_cost(r),usage=r.get('response',{}).get('usage'),stop=r.get('response',{}).get('stop_reason'),selected=s['selected']);rows.append(v)
assert len(sel)==768 and len({(r['job']['id'],r['job']['model'],r['job']['replicate']) for r in rows})==768
by_rep={}
for rep in [1,2,3]:
 out={}
 for policy in ['max','flash','auto','semantic']:
  vs=[bank[t,policy if policy in ['max','flash'] else routes[t][policy],rep] for t in M['tasks']]
  out[policy]={'passed':sum(v['pass'] for v in vs),'n':128,'generation_cny_known':sum(v['cost'] or 0 for v in vs),'generation_usage_unknown':sum(v['cost'] is None for v in vs),'http_status':dict(collections.Counter(v['status'] for v in vs)),'max_tokens':sum(v['stop']=='max_tokens' for v in vs)}
 out['sr_minus_jev_pass']=out['semantic']['passed']-out['auto']['passed'];out['jev_minus_sr_cny_known']=out['auto']['generation_cny_known']-out['semantic']['generation_cny_known'];by_rep[str(rep)]=out
per_task=[]
for t in M['tasks']:
 r={'id':t,'routes':routes[t],'models':{}}
 for model in ['max','flash']:
  vs=[bank[t,model,k] for k in [1,2,3]];r['models'][model]={'passed':[v['pass'] for v in vs],'successes':sum(v['pass'] for v in vs),'costs':[v['cost'] for v in vs],'unique_code_count':len({v['code_sha256'] for v in vs})}
 r['sr_minus_jev_mean_pass']=(r['models'][routes[t]['semantic']]['successes']-r['models'][routes[t]['auto']]['successes'])/3
 j=[bank[t,routes[t]['auto'],k]['cost'] for k in [1,2,3]];s=[bank[t,routes[t]['semantic'],k]['cost'] for k in [1,2,3]];r['jev_minus_sr_mean_cny']=sum(a-b for a,b in zip(j,s))/3 if all(v is not None for v in j+s) else None;per_task.append(r)
selection=json.loads((D/'selection.json').read_text());strata={}
for r in selection:strata.setdefault(r['platform']+'/'+r['difficulty'],[]).append(r['id'])
taskmap={r['id']:r for r in per_task};rng=random.Random(2026101505);boots=[];costboots=[]
for _ in range(10000):
 ids=[rng.choice(ts) for ts in strata.values() for __ in ts];boots.append(sum(taskmap[t]['sr_minus_jev_mean_pass'] for t in ids)/128)
 if all(taskmap[t]['jev_minus_sr_mean_cny'] is not None for t in ids):costboots.append(sum(taskmap[t]['jev_minus_sr_mean_cny'] for t in ids))
boots.sort();costboots.sort();attempts=[json.loads(p.read_text()) for p in (D/'attempts').glob('*.json') if not p.name.endswith('.started.json')]
stats={'replicates':by_rep,'mean_sr_minus_jev_pass_count':statistics.mean(r['sr_minus_jev_pass'] for r in by_rep.values()),'mean_jev_minus_sr_cny_known':statistics.mean(r['jev_minus_sr_cny_known'] for r in by_rep.values()),'task_stratified_bootstrap_95_pass_rate_difference':[boots[249],boots[9749]],'task_stratified_bootstrap_95_mean_batch_saving':[costboots[249],costboots[9749]] if len(costboots)==10000 else None,'interpretation':'Conditional on frozen task set, routes and3 fresh samples per model. Task-stratified bootstrap resamples3-sample task means; not strong evidence about long-run model randomness or route-decision stability.','actual_bank_attempts':len(attempts),'actual_bank_generation_cny_known':sum(usage_cost(r) or 0 for r in attempts),'actual_bank_unknown_usage':sum(usage_cost(r) is None for r in attempts),'actual_attempt_status':dict(collections.Counter(r['status'] for r in attempts)),'policy_sum_warning':'Policy replay fees are hypothetical selections from one shared bank, not additional paid calls. No new Jev or SR routing decisions were invoked.','model_success_histograms':{m:dict(collections.Counter(r['models'][m]['successes'] for r in per_task)) for m in ['max','flash']}}
stats['different_route_task_evidence']={'n':sum(r['routes']['auto']!=r['routes']['semantic'] for r in per_task),'sr_higher_empirical_success':sum(r['sr_minus_jev_mean_pass']>0 for r in per_task),'jev_higher_empirical_success':sum(r['sr_minus_jev_mean_pass']<0 for r in per_task),'ties_among_different_routes':sum(r['routes']['auto']!=r['routes']['semantic'] and r['sr_minus_jev_mean_pass']==0 for r in per_task)}
stats['replay_all_attempt_costs']={}
for rep in [1,2,3]:
 out={}
 for policy in ['max','flash','auto','semantic']:
  selected_attempts=[r for r in attempts if r['job']['replicate']==rep and r['job']['model']==(policy if policy in ['max','flash'] else routes[r['job']['id']][policy])]
  out[policy]={'attempts':len(selected_attempts),'generation_cny_known':sum(usage_cost(r) or 0 for r in selected_attempts),'unknown_usage_attempts':sum(usage_cost(r) is None for r in selected_attempts)}
 stats['replay_all_attempt_costs'][str(rep)]=out
(D/'bank.json').write_text(json.dumps(rows,indent=2));(D/'per-task.json').write_text(json.dumps(per_task,indent=2));(D/'summary.json').write_text(json.dumps(stats,indent=2));print(json.dumps(stats,indent=2))
