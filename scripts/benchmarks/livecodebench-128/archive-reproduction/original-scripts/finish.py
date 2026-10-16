import pathlib,time,subprocess
D=pathlib.Path(__file__).parent;started=time.monotonic()
while not ((D/'run.log').exists() and (D/'run.log').read_text().rstrip().endswith('GENERATION_COMPLETE')):
 if time.monotonic()-started>36000:raise RuntimeError('Generation did not complete within10h; no generation repeated by finalizer')
 time.sleep(15)
print('START_SCORING',flush=True)
for script in ['score_all.py','verify.py','summarize.py','report.py','archive.py']:
 subprocess.run(['python3',str(D/script)],check=True)
print('EXPERIMENT_COMPLETE',flush=True)
