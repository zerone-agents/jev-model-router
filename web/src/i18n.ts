export type Lang='en'|'zh';
export const text=(lang:Lang,en:string,zh:string)=>lang==='zh'?zh:en;
export const labels={en:{overview:'Overview',models:'Models',prompt:'Routing prompt',records:'Routing records'},zh:{overview:'概览',models:'模型',prompt:'路由提示词',records:'路由记录'}};
