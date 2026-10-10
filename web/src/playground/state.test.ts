import {expect,it} from 'vitest';
import {initialState,reducePlayground,history} from './state';
it('never returns to thinking after content and keeps reasoning separate',()=>{
 let s=reducePlayground(initialState(),{type:'start',id:1,prompt:'hi'});
 expect(s.phase).toBe('waiting');s=reducePlayground(s,{type:'event',id:1,event:{type:'delta',reasoning_content:'hidden'}});expect(s.phase).toBe('thinking');
 s=reducePlayground(s,{type:'event',id:1,event:{type:'delta',content:'visible'}});s=reducePlayground(s,{type:'event',id:1,event:{type:'delta',reasoning_content:'more'}});expect(s.phase).toBe('responding');expect(s.turns[0].content).toBe('visible');
 s=reducePlayground(s,{type:'event',id:1,event:{type:'done',finish_reason:'stop'}});expect(s.phase).toBe('completed');expect(history(s)).toEqual([{role:'user',content:'hi'},{role:'assistant',content:'visible',reasoning_content:'hiddenmore'}]);
});
it('ignores late events and excludes partial pairs from history',()=>{
 let s=reducePlayground(initialState(),{type:'start',id:1,prompt:'hi'});s=reducePlayground(s,{type:'event',id:1,event:{type:'delta',content:'partial'}});s=reducePlayground(s,{type:'stop',id:1});expect(history(s)).toEqual([]);expect(s.turns[0].content).toBe('partial');
 s=reducePlayground(s,{type:'event',id:1,event:{type:'delta',content:'late'}});expect(s.turns[0].content).toBe('partial');s=reducePlayground(s,{type:'reset'});s=reducePlayground(s,{type:'event',id:1,event:{type:'done',finish_reason:'stop'}});expect(s.turns).toEqual([]);
});
