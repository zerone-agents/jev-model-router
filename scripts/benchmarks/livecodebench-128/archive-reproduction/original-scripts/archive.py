import pathlib,json,hashlib,shutil,re
D=pathlib.Path(__file__).parent;ROOT=pathlib.Path('/Users/zero/.codex/worktrees/benchmark-promotion/jev-model-router/.data');T=ROOT/'benchmark-shared-3x-2026-10-15';assert not T.exists();old={}
for n in ['benchmark-expanded-128-2026-10-15','routing-review-expanded-2026-10-15']:
 B=ROOT/n;h=json.loads((B/'sha256.json').read_text())
 for f,digest in h.items():
  z=hashlib.sha256()
  with (B/f).open('rb') as stream:
   for b in iter(lambda:stream.read(1024*1024),b''):z.update(b)
  assert z.hexdigest()==digest,(n,f)
 old[n]={'verified_files':len(h),'changed_files':0,'manifest_sha256':hashlib.sha256((B/'sha256.json').read_bytes()).hexdigest()}
(D/'source-integrity.json').write_text(json.dumps(old,indent=2));files=[p for p in D.rglob('*') if p.is_file() and '__pycache__' not in p.parts];keys=[p.read_bytes().strip() for p in pathlib.Path('/private/tmp/jev-benchmark-RxrO56').glob('*.key') if p.stat().st_size]
for p in files:
 b=p.read_bytes();assert not any(k in b for k in keys),p
 if p.suffix=='.md' and not re.search(r'(\.zh|\.zh-cn|-zh)\.md$',p.name,re.I):assert not re.search('[\u4e00-\u9fff]',b.decode()),p
for p in files:
 q=T/p.relative_to(D);q.parent.mkdir(parents=True,exist_ok=True);shutil.copy2(p,q)
(T/'archive-validation.json').write_text(json.dumps({'prior_archives':old,'credential_hits':0,'markdown_language_check':'passed','copied_files':len(files)},indent=2));(T/'sha256.json').write_text(json.dumps({str(p.relative_to(T)):hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(T.rglob('*')) if p.is_file()},indent=2));print('ARCHIVED',T,flush=True)
