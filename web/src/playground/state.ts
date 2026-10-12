import type { Diagnostic, Message, PlaygroundEvent, Route } from "./client";
export type Phase =
  | "idle"
  | "waiting"
  | "thinking"
  | "responding"
  | "completed"
  | "stopped"
  | "failed"
  | "truncated";
export type Turn = {
  id: number;
  prompt: string;
  content: string;
  reasoning: string;
  phase: Phase;
  route?: Route;
  diagnostic?: Diagnostic;
  errorCode?: string;
  limitScope?: string;
  usage?: {
    input_tokens: number;
    output_tokens: number;
    total_tokens?: number;
  };
};
export type PlaygroundState = {
  id: number | null;
  phase: Phase;
  turns: Turn[];
};
export type PlaygroundAction =
  | { type: "start"; id: number; prompt: string }
  | { type: "event"; id: number; event: PlaygroundEvent }
  | {
      type: "stop" | "fail";
      id: number;
      diagnostic?: Diagnostic;
      errorCode?: string;
      limitScope?: string;
    }
  | { type: "reset" };
export const initialState = (): PlaygroundState => ({
  id: null,
  phase: "idle",
  turns: [],
});
export const isActive = (phase: Phase) =>
  ["waiting", "thinking", "responding"].includes(phase);
export function reducePlayground(
  s: PlaygroundState,
  a: PlaygroundAction,
): PlaygroundState {
  if (a.type === "reset") return initialState();
  if (a.type === "start")
    return {
      id: a.id,
      phase: "waiting",
      turns: [
        ...s.turns,
        {
          id: a.id,
          prompt: a.prompt,
          content: "",
          reasoning: "",
          phase: "waiting",
        },
      ],
    };
  if (a.id !== s.id || !isActive(s.phase)) return s;
  const turn = { ...s.turns[s.turns.length - 1] };
  if (a.type === "stop" || a.type === "fail") {
    turn.phase = a.type === "stop" ? "stopped" : "failed";
    if (a.type === "fail") {
      turn.errorCode = a.errorCode || "internal_error";
      turn.limitScope = a.limitScope;
      turn.diagnostic = a.diagnostic;
    }
  } else if (a.type === "event") {
    const e = a.event;
    if (e.type === "route") turn.route = e;
    if (e.type === "delta") {
      turn.content += e.content || "";
      turn.reasoning += e.reasoning_content || "";
      turn.phase = turn.content
        ? "responding"
        : turn.reasoning
          ? "thinking"
          : "waiting";
    }
    if (e.type === "done") {
      turn.phase =
        e.finish_reason === "stop" && turn.content
          ? "completed"
          : e.finish_reason === "length"
            ? "truncated"
            : "failed";
      turn.usage = e.usage;
    }
    if (e.type === "error") {
      turn.phase = "failed";
      turn.errorCode = e.code;
      turn.diagnostic = e;
    }
  }
  return { ...s, phase: turn.phase, turns: [...s.turns.slice(0, -1), turn] };
}
export function history(s: PlaygroundState): Message[] {
  return s.turns.flatMap((t) =>
    t.phase === "completed"
      ? [
          { role: "user" as const, content: t.prompt },
          {
            role: "assistant" as const,
            content: t.content,
            ...(t.reasoning ? { reasoning_content: t.reasoning } : {}),
          },
        ]
      : [],
  );
}
