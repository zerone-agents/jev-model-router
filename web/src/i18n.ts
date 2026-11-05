export type Lang = "en" | "zh";
export const text = (lang: Lang, en: string, zh: string) =>
  lang === "zh" ? zh : en;
export const labels = {
  en: {
    overview: "Overview",
    playground: "Playground",
    models: "Models",
    prompt: "Routing prompt",
    records: "Routing records",
    privacyPrompt: "Privacy prompt",
    privacyRecords: "Privacy records",
  },
  zh: {
    overview: "概览",
    playground: "Playground",
    models: "模型",
    prompt: "路由提示词",
    records: "路由记录",
    privacyPrompt: "隐私提示词",
    privacyRecords: "隐私记录",
  },
};
