# Model routing

Select an appropriate model for a generation request while constraining where its data may be sent.

## Language

**Decision Backend**: A model service, such as Jev or Laya, that understands a task and returns typed judgments.
_Avoid_: Calling the decision backend a generation model.

**Generation Model**: A routing target that produces the actual reply or tool call.

**Provider**: A service or local deployment defining access to generation models and their credential boundary.

**Model Description**: Natural-language guidance for the decision backend about a generation model's capabilities, specialties, weaknesses, cost and speed.
_Avoid_: Calling it a model card that requires a training report, or substituting it for verifiable capability declarations.

**Model Capability**: Supported input, context range, output formats and tool-protocol constraints, distinct from task specialization.

**Eligible Models**: Generation models satisfying the current request's permission, deployment-location and capability constraints.

**Routing Prompt**: Editable preferences for comparing eligible models, with balanced selection as the default.

**Auto Selection**: The caller delegates model selection to the router.

**Explicit Selection**: The caller names a configured model ID; deployment and capability constraints still apply.

**Privacy Routing**: An optional capability that constrains generation destinations according to context sensitivity.

**Local-only Session**: Subsequent requests must remain local because context is sensitive or cannot be reliably classified.
_Avoid_: Treating every use of a local generation model as a local-only session.

**Route Inspection**: Select a model without generation. It can still call a decision service, incur cost and send data externally.
_Avoid_: Describing inspection as a purely local preview or a zero-side-effect dry run.

**Management Capability**: One discoverable business operation for an Agent or human to inspect or adjust the router, with explicit inputs, results and side effects.

**Inference Role**: Can use every enabled generation model in the instance, through Auto or explicit selection, without configuration-management permissions.

**Settings Role**: A trusted instance administrator who manages configuration, reads status, inspects routing and performs bounded connection tests. It has no general generation permission but can change subsequent data destinations. Dashboard Cookie sessions can use the separate, rate-limited Playground text-generation endpoint; this exception does not extend Settings Bearer or /v1 inference permissions.
_Avoid_: Equating these roles with security isolation between mutually untrusted tenants.

**Model Connection Test**: A management operation sending a fixed minimal request to check basic availability. It can test a disabled model and can incur cost and external data transfer.
_Avoid_: Equating connection success with validation of all declared capabilities, or equating a test with enabling the model.

**Routing Record**: Request-level selection and execution metadata for troubleshooting and evaluation, including at most 120 characters from the last user message. It excludes full conversations, tool arguments/results, image addresses and free-text decision explanations.
_Avoid_: Calling best-effort records mandatory auditing.

**Selection Path**: How the model was selected: explicit caller choice, the sole eligible candidate, or the decision backend.
_Avoid_: Treating the path or candidate probability as a natural-language decision explanation.

**Generation Health**: Observed basic availability of an actual call target: unknown, healthy or isolated. It is separate from configuration enablement and answer quality. Isolation excludes Auto candidates; explicit calls remain allowed.

**Credential Snapshot**: An immutable credential resolution shared by actual authentication, health identity, client reuse and error redaction. Core code holds an opaque handle and never serializes the credential.

**Recovery Probe**: A bounded background task sending a fixed small request to an enabled, isolated target. A complete successful call restores eligibility; elapsed time alone does not. Manual models.test can test disabled targets without enabling them.

**Auto Fallback**: At most one additional selection and generation attempt before semantic output, excluding the failed model while preserving request input and configuration version. It is distinct from largest-context estimate fallback and SDK retries.

**Request Budget Exhaustion**: Local termination of the entire inference lifecycle. It is publicly a timeout but neither increments nor resets generation-health observations.
