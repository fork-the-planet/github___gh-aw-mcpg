# Work IQ Guard: DIFC Plan for Microsoft 365 Data

## 1. Context

### What is Work IQ?

The [Work IQ MCP server](https://github.com/microsoft/work-iq-mcp) exposes Microsoft 365 data to AI coding assistants via the Model Context Protocol. It provides natural-language access to:

| Data Type | Example Query |
|-----------|---------------|
| **Emails** | "What did John say about the proposal?" |
| **Meetings** (calendar + transcripts) | "What's on my calendar tomorrow?" |
| **Documents** (SharePoint/OneDrive) | "Find my recent PowerPoint presentations" |
| **Teams messages** (chat + channels) | "Summarize today's messages in the Engineering channel" |
| **People** | "Who is working on Project Alpha?" |

Work IQ uses the Microsoft 365 Copilot Chat API with these delegated Graph permissions:

| Permission | Data Scope |
|-----------|-----------|
| `Sites.Read.All` | SharePoint/OneDrive documents |
| `Mail.Read` | User mailbox |
| `People.Read.All` | People/org directory |
| `OnlineMeetingTranscript.Read.All` | Meeting transcripts |
| `Chat.Read` | Teams 1:1/group chat |
| `ChannelMessage.Read.All` | Teams channel messages |
| `ExternalItem.Read.All` | External connectors |

### What the GitHub Guard Does (recap)

The existing GitHub Guard implements DIFC with two label dimensions:

- **Secrecy**: `[] (public) < private:<owner> < private:<owner>/<repo> < secret` — based on repository visibility
- **Integrity**: `none < unapproved < approved < merged` — based on Git branching/merging model with author-association endorsement

The guard exposes three WASM entrypoints (`label_agent`, `label_resource`, `label_response`) and labels every tool call and response item so the gateway can enforce information-flow rules.

### Why a Separate Guard?

M365 data has fundamentally different ownership, sharing, and endorsement models than GitHub. A Work IQ guard must map DIFC concepts to M365-native primitives rather than forcing GitHub concepts onto it.

---

## 2. Key Differences: GitHub vs M365

| Concept | GitHub | M365 |
|---------|--------|------|
| **Ownership unit** | Repository (owner/repo) | Tenant, with sites/groups/users as sub-scopes |
| **Visibility** | Public / Private (binary per repo) | Complex: personal, group-scoped, site-scoped, org-wide, external sharing |
| **Access control** | Repo collaborator roles | Entra ID groups, Teams membership, SharePoint permissions, sensitivity labels |
| **Endorsement model** | Branching + merging + author-association | No branches/merges; authorship, organizational role, document lifecycle (draft→published), sensitivity labels |
| **Content provenance** | Git commit graph, PR lineage, default-branch reachability | Author identity, tenant membership, sharing chain, meeting attendance |
| **Groups** | GitHub Organizations / Teams | M365 Groups, Security Groups, Distribution Lists, Teams |

---

## 3. Secrecy Model for M365

### 3.1 Design Principle

In GitHub, secrecy derives from one signal: repository visibility (`public` vs `private`). In M365, secrecy must capture **audience scope** — who is supposed to see this content — which comes from multiple overlapping signals.

### 3.2 Secrecy Levels

We define a secrecy lattice with the following tags, from least to most restrictive:

```
[] (org-wide / unrestricted within tenant)
  < group:<group-id>
  < personal:<user-upn>
  < confidential:<label-id>
  < highly-confidential:<label-id>
```

#### Tag Definitions

| Secrecy Tag | Meaning | M365 Source Signal |
|---|---|---|
| `[]` (empty) | Accessible to all members of the tenant; no audience restriction beyond org membership | Org-wide SharePoint sites, company-wide distribution lists, public Teams channels |
| `group:<group-id>` | Scoped to members of a specific M365 Group, Team, or security group | Teams private/shared channels, Team-scoped SharePoint sites, group mailboxes |
| `personal:<user-upn>` | Scoped to a specific user's personal data | User mailbox, personal OneDrive, private calendar events |
| `confidential:<label-id>` | Content bearing a "Confidential" (or equivalent) Microsoft Purview sensitivity label | Sensitivity label metadata from Graph API |
| `highly-confidential:<label-id>` | Content bearing a "Highly Confidential" (or equivalent) sensitivity label | Sensitivity label metadata from Graph API |

#### Important: Groups Are First-Class Secrecy Scopes

Unlike GitHub where private data is scoped to `owner/repo`, M365 data is often scoped to **groups**. A Teams channel message is private to the Team members. A SharePoint team site document is private to the M365 Group members. The `group:<group-id>` tag captures this:

- The `<group-id>` is the M365 Group object ID (GUID) from Entra ID.
- A Teams team maps 1:1 to an M365 Group, so `group:<team-group-id>` covers all Team data.
- Shared channels that span multiple teams would carry multiple `group:` tags (one per participating team).
- Security groups used for SharePoint permission grants also map to `group:` tags.

#### Hierarchy Expansion

When assigning secrecy, the guard expands to include broader scopes:

| When assigning... | Guard emits... |
|---|---|
| `[]` | `[]` |
| `group:<group-id>` | `["group:<group-id>"]` |
| `personal:<user-upn>` | `["personal:<user-upn>"]` |
| `confidential:<label-id>` | `["confidential:<label-id>"]` plus any applicable `group:` or `personal:` tags |
| `highly-confidential:<label-id>` | `["highly-confidential:<label-id>"]` plus any applicable `group:` or `personal:` tags |

Sensitivity labels are **additive** — a document in a private Team site with a "Confidential" sensitivity label carries **both** `group:<id>` and `confidential:<label-id>`.

### 3.3 Secrecy by M365 Data Type

| Data Type | Signal Source | Secrecy Assignment |
|---|---|---|
| **Email (user mailbox)** | Always user-personal | `["personal:<user-upn>"]` |
| **Email (shared mailbox)** | Shared mailbox membership = group | `["group:<shared-mailbox-group-id>"]` |
| **Calendar event (private)** | User's personal calendar, `sensitivity: private` | `["personal:<user-upn>"]` |
| **Calendar event (normal)** | User's calendar, others can see details | `["personal:<user-upn>"]` (conservative default) |
| **Meeting transcript** | Meeting with specific attendees | `["group:<meeting-group-id>"]` if Team meeting; else `["personal:<organizer-upn>"]` |
| **OneDrive file (personal)** | User's personal OneDrive | `["personal:<user-upn>"]` |
| **OneDrive file (shared)** | Shared via link/permission | `["personal:<user-upn>"]` (owner), but note sharing expands audience |
| **SharePoint site (team)** | M365-Group-connected site | `["group:<group-id>"]` |
| **SharePoint site (communication)** | Org-wide communication site | `[]` or `["group:<site-members-group>"]` depending on permissions |
| **Teams channel message (standard)** | Standard channel in a Team | `["group:<team-group-id>"]` |
| **Teams channel message (private)** | Private channel (subset of Team) | `["group:<private-channel-group-id>"]` |
| **Teams chat (1:1)** | Direct chat between two users | `["personal:<user-upn>"]` for each participant |
| **Teams chat (group)** | Group chat among N users | `["personal:<user-upn>"]` for each participant (no formal group) |
| **People/directory info** | Org directory (GAL) | `[]` (org-wide) |
| **External items** | External connector data | Depends on connector; default `["group:<connector-scope>"]` or fail-closed |
| **Any item with sensitivity label** | Microsoft Purview label metadata | Add `confidential:<label-id>` or `highly-confidential:<label-id>` on top of audience scope |

### 3.4 Flow Rule

Secrecy enforces confidentiality:

- An agent may consume data only if its secrecy clearance is a **superset** of the data's secrecy tags.
- Writing data to a destination must not reduce confidentiality (no downgrade).

### 3.5 Sensitivity Label Integration

Microsoft Purview sensitivity labels (e.g., Public, Internal, Confidential, Highly Confidential) are a native M365 concept. The guard should:

1. **Read sensitivity label metadata** from Graph API responses when present.
2. **Map labels to secrecy tags** using a configurable mapping in the guard policy (since label names/IDs are tenant-specific).
3. **Treat sensitivity labels as additive** — they layer on top of audience-scope secrecy, never replace it.

Example policy mapping:
```json
{
  "sensitivity_label_map": {
    "Public": [],
    "Internal": [],
    "Confidential": "confidential",
    "Highly Confidential": "highly-confidential"
  }
}
```

---

## 4. Integrity Model for M365

### 4.1 Design Principle

In GitHub, integrity derives from the Git contribution model: untrusted external contributions gain trust through code review and merging into the default branch. M365 has no branching/merging, so we need a different model of endorsement.

**Key insight**: M365 integrity should be based on **organizational trust** and **content lifecycle** rather than branch/merge workflows.

### 4.2 What Replaces Branching and Merging?

| GitHub Concept | M365 Equivalent | Rationale |
|---|---|---|
| **Merged to default branch** | **Published / finalized content** | A SharePoint document in a published version, a sent email, or an official meeting recording represents finalized content analogous to merged code |
| **Writer-contributed (direct PR)** | **Internal-authored content** | Content authored by a tenant member (employee) with write permissions to the resource, analogous to a repo collaborator's direct PR |
| **Reader-contributed (forked PR)** | **Guest/external-authored content** | Content authored by an external guest user, analogous to a fork-based PR from someone without write access |
| **None (untrusted)** | **Anonymous / external-shared content** | Content from anonymous links, external connectors with unknown provenance, or forwarded content with broken attribution |

### 4.3 Integrity Levels

```
published:<scope> (highest)
  > internal:<scope>
  > guest:<scope>
  > none:<scope> (lowest)
```

#### Tag Definitions

| Integrity Tag | Meaning | M365 Source Signal |
|---|---|---|
| `published:<scope>` | Content that has been finalized/published through an official process | SharePoint published page versions, sent emails, completed/recorded meetings, approved documents |
| `internal:<scope>` | Content authored by an internal tenant member | Author UPN is in the tenant directory; user is not a guest |
| `guest:<scope>` | Content authored by an external guest user | Author is a B2B guest in Entra ID (`#EXT#` UPN format or `userType=Guest`) |
| `none:<scope>` | No verifiable authorship or provenance | Anonymous access, broken forwarding chains, external connector data without attribution |

#### Scope

`<scope>` identifies the organizational boundary:

| Scope | Meaning | Example |
|---|---|---|
| `<tenant-id>` | Tenant-wide scope for cross-resource data | `internal:contoso.onmicrosoft.com` |
| `<group-id>` | Group/Team scope for group-specific data | `published:team-engineering-abc123` |
| `<site-id>` | SharePoint site scope | `internal:site-hr-portal-def456` |

#### Hierarchy Expansion

Expansion follows the same pattern as GitHub:

| When assigning... | Guard emits... |
|---|---|
| `none:<scope>` | `["none:<scope>"]` |
| `guest:<scope>` | `["none:<scope>", "guest:<scope>"]` |
| `internal:<scope>` | `["none:<scope>", "guest:<scope>", "internal:<scope>"]` |
| `published:<scope>` | `["none:<scope>", "guest:<scope>", "internal:<scope>", "published:<scope>"]` |

### 4.4 Integrity by M365 Data Type

| Data Type | Signal Source | Integrity Assignment |
|---|---|---|
| **Email (from internal sender)** | Sender UPN in tenant, message is sent (finalized) | `published:<tenant>` — sent email is a finalized artifact |
| **Email (from external sender)** | Sender domain is external | `guest:<tenant>` — external mail is guest-level |
| **Email (forwarded, attribution broken)** | Forwarding chain loses original author | `none:<tenant>` — provenance uncertain |
| **Meeting transcript (recorded meeting)** | Official Teams meeting recording with attendee list | `published:<group-id>` if Team meeting; `published:<tenant>` otherwise |
| **Meeting transcript (manual notes)** | Copilot-generated or user-generated notes | `internal:<tenant>` if author is internal |
| **SharePoint document (published version)** | `_UIVersionString` indicates major/published version | `published:<site-id>` |
| **SharePoint document (draft)** | Minor version or checked-out document | `internal:<site-id>` if author is internal, `guest:<site-id>` if guest |
| **SharePoint document (externally authored)** | Guest user uploaded/edited | `guest:<site-id>` |
| **OneDrive file** | Personal file, author is owner | `internal:<tenant>` (personal files are internal-authored but not formally published) |
| **Teams channel message (from member)** | Message author is Team member (internal) | `internal:<group-id>` |
| **Teams channel message (from guest)** | Message author is a guest in the Team | `guest:<group-id>` |
| **Teams chat message** | Direct/group chat | `internal:<tenant>` or `guest:<tenant>` based on author |
| **People/directory info** | Entra ID directory | `published:<tenant>` — directory data is system-managed/canonical |
| **External connector items** | Third-party data via Graph connectors | `none:<tenant>` (default; configurable per connector) |

### 4.5 Flow Rule

Integrity enforces trust:

- An agent can only consume data at or above its integrity floor.
- An agent cannot produce output that claims higher integrity than its inputs warrant.

### 4.6 Why Not Branches/Merges?

M365 content does not flow through a branch→review→merge pipeline. Instead:

- **Documents** have a version history with draft/published states (analogous to feature-branch vs merged).
- **Emails** are either sent (finalized, immutable) or drafts (in-progress). Sent email is analogous to "merged" — it's a committed, immutable artifact.
- **Chat messages** are fire-and-forget; there's no review/merge step. Trust comes from author identity, not workflow endorsement.
- **Meeting transcripts** are system-generated from recorded meetings — they carry high integrity because they're automatically captured by the platform.

The `published` level captures the "merged" equivalent: content that has passed through an official finalization step and is immutable or officially versioned.

---

## 5. Tool Classification

Work IQ currently exposes a single tool (`ask_workiq` or similar) that accepts natural-language queries and returns mixed-type results. This complicates traditional tool-level classification because a single query can return emails, documents, and messages together.

### 5.1 Resource-Level Labels (label_resource)

Since Work IQ queries are read-only (all Graph permissions are `*.Read.*`), all operations are `read`:

| Tool | Operation | Resource Secrecy | Resource Integrity |
|---|---|---|---|
| `ask_workiq` (email query) | read | `["personal:<user-upn>"]` | `none:<tenant>` (coarse; response refines) |
| `ask_workiq` (meeting query) | read | `["personal:<user-upn>"]` | `none:<tenant>` (coarse; response refines) |
| `ask_workiq` (document query) | read | `[]` (coarse; response refines) | `none:<tenant>` (coarse; response refines) |
| `ask_workiq` (teams query) | read | `[]` (coarse; response refines) | `none:<tenant>` (coarse; response refines) |
| `ask_workiq` (people query) | read | `[]` | `published:<tenant>` |

Because Work IQ multiplexes data types through a single tool, resource labels must be **conservatively coarse** and response labeling does the authoritative per-item work.

### 5.2 Response Labels (label_response)

The guard must parse Work IQ response payloads and identify per-item types to apply fine-grained labels. Key signals in the response:

| Signal | Where to Find | Used For |
|---|---|---|
| Item type (email, event, file, message, person) | Response structure / `@odata.type` | Route to type-specific labeling rules |
| Author/sender identity | `from`, `sender`, `createdBy`, `lastModifiedBy` | Integrity (internal vs guest) |
| Author tenant membership | UPN domain, `#EXT#` suffix, `userType` | Integrity (internal vs guest vs anonymous) |
| Container (site, team, group) | `parentReference`, `channelIdentity`, `chatId` | Secrecy (group scope) |
| Sensitivity label | `sensitivityLabel`, `sensitivity` metadata | Secrecy (confidential/highly-confidential) |
| Document version status | `_UIVersionString`, `publicationLevel` | Integrity (published vs draft) |
| Email sent status | `isDraft` flag | Integrity (published if sent, lower if draft) |
| Meeting recording status | Presence of transcript, recording metadata | Integrity (published if recorded) |

---

## 6. Agent Policy (label_agent)

### 6.1 Policy Shape

Analogous to the GitHub guard's `AllowOnly` policy, a Work IQ policy controls what data the agent may consume:

```json
{
  "AllowOnly": {
    "Scope": "Tenant",
    "min-integrity": "none",
    "Groups": ["<group-id-1>", "<group-id-2>"]
  }
}
```

#### Field Semantics

| Field | Values | Meaning |
|---|---|---|
| `AllowOnly.Scope` | `"Tenant"`, `"Personal"`, `{ "group": "<group-id>" }` | Scope restriction — analogous to `Repos` in GitHub guard |
| `AllowOnly.min-integrity` | `"None"`, `"Guest"`, `"Internal"`, `"Published"` | Minimum integrity floor for consumed data |
| `AllowOnly.Groups` | Array of group IDs (optional) | Explicit allow-list of groups whose data the agent may access |

#### Scope Semantics

| Scope Value | Agent Secrecy Clearance | Effect |
|---|---|---|
| `"Tenant"` | `["personal:<user-upn>", "group:*"]` (all groups user is a member of) | Agent can see all data the user can see |
| `"Personal"` | `["personal:<user-upn>"]` only | Agent can only see user's personal data (mailbox, OneDrive, calendar) |
| `{ "group": "<group-id>" }` | `["group:<group-id>"]` | Agent can only see data scoped to one specific group/team |

#### Groups Allow-List

The optional `Groups` array provides fine-grained control:

- When specified, the agent can only consume data tagged with `group:<id>` where `<id>` is in the allow-list.
- This prevents the agent from accessing data in Teams/groups outside the relevant project scope.
- Absent `Groups` field means all groups the user is a member of are accessible.

### 6.2 label_agent Output

```json
{
  "agent": {
    "secrecy": ["personal:user@contoso.com", "group:abc-123"],
    "integrity": ["none:contoso.onmicrosoft.com", "guest:contoso.onmicrosoft.com"]
  },
  "difc_mode": "filter",
  "normalized_policy": {
    "scope_kind": "tenant|personal|group",
    "integrity": "None|Guest|Internal|Published",
    "allowed_groups": ["abc-123"]
  }
}
```

---

## 7. Mapping Summary: GitHub Guard → Work IQ Guard

| Aspect | GitHub Guard | Work IQ Guard |
|--------|-------------|---------------|
| **Secrecy dimension** | Repo visibility: `[] / private:<owner>/<repo> / secret` | Audience scope: `[] / group:<id> / personal:<upn> / confidential:<label> / highly-confidential:<label>` |
| **Integrity dimension** | Contribution model: `none / unapproved / approved / merged` | Org trust + lifecycle: `none / guest / internal / published` |
| **Integrity scope** | `<owner>/<repo>` | `<tenant-id>`, `<group-id>`, or `<site-id>` |
| **Secrecy scope** | `<owner>`, `<owner>/<repo>` | `<user-upn>`, `<group-id>`, `<label-id>` |
| **"Merged" equivalent** | Default-branch reachable commits/PRs | Published documents, sent emails, recorded meetings |
| **"Writer" equivalent** | Repo collaborator with push access | Internal tenant member |
| **"Reader" equivalent** | Fork-based contributor | External guest user |
| **"None" equivalent** | Unknown author, no merged PRs | Anonymous, broken provenance, external connectors |
| **Group concept** | GitHub Organization / Teams (not directly in secrecy model) | M365 Groups as first-class secrecy scopes (`group:<id>`) |
| **Policy scope** | `AllowOnly.Repos: Public / {owner} / {owner,repo}` | `AllowOnly.Scope: Tenant / Personal / {group}` + `AllowOnly.Groups: [...]` |
| **Tool classification** | ~40 distinct tools with clear read/write/delete ops | Single multiplexed tool (`ask_workiq`), read-only; response parsing needed |
| **Sensitivity labels** | N/A | Mapped to secrecy tags via configurable policy |

---

## 8. Implementation Plan

### Phase 1: Foundation

1. **Define Rust data structures** for M365 secrecy and integrity tags in a new `workiq-guard/` crate.
2. **Implement `label_agent`** with M365 policy parsing and normalization.
3. **Implement `label_resource`** with coarse labeling for the `ask_workiq` tool based on query heuristics (email vs meeting vs document vs teams vs people).
4. **Implement response parsing** to identify M365 item types from Work IQ response payloads.
5. **Implement `label_response`** with per-item secrecy and integrity assignment.

### Phase 2: Secrecy

6. **Audience-scope secrecy**: Derive `personal:` / `group:` / `[]` from response metadata.
7. **Sensitivity label integration**: Read sensitivity label metadata from responses; map to `confidential:` / `highly-confidential:` via policy config.
8. **Group resolution**: Resolve Team/channel/site to M365 Group IDs for `group:` tags.

### Phase 3: Integrity

9. **Author classification**: Determine internal vs guest vs anonymous from author UPN and tenant membership.
10. **Lifecycle classification**: Determine published vs draft status from document versions, email sent status, meeting recording status.
11. **Scope derivation**: Assign appropriate scope (tenant/group/site) based on content container.

### Phase 4: Policy & Testing

12. **Policy schema**: Define JSON Schema for Work IQ `AllowOnly` policy.
13. **Unit tests**: Label derivation tests for each data type × secrecy × integrity combination.
14. **Integration tests**: End-to-end tests with mock Work IQ responses.
15. **Documentation**: Spec documents analogous to `SECRECY_TAG_SPEC.md` and `INTEGRITY_TAG_SPEC.md`.

### Phase 5: Advanced

16. **Multi-group secrecy**: Handle items accessible to multiple groups (shared channels, cross-posted content).
17. **Sensitivity label hierarchy**: Support tenant-specific label priority ordering.
18. **External connector policies**: Configurable integrity/secrecy defaults per external connector source.
19. **Query classification**: Heuristic or LLM-assisted classification of natural-language queries to improve resource-level labels.

---

## 9. Open Questions

1. **Work IQ response format**: The proprietary Work IQ server's response schema is undocumented. The guard will need to reverse-engineer or negotiate the response format to extract item types, author metadata, and container information. This is the primary implementation risk.

2. **Sensitivity label ID resolution**: Sensitivity label names are tenant-specific. Should the guard accept label GUIDs, display names, or both in the policy mapping?

3. **Group explosion**: A user may be a member of hundreds of M365 Groups. Should the agent's secrecy clearance enumerate all groups, or should the policy require explicit allow-listing?

4. **Single-tool multiplexing**: Work IQ routes all queries through one tool. If future versions expose dedicated per-data-type tools (e.g., `search_emails`, `search_documents`, `get_calendar`), the guard should be ready to classify each independently.

5. **External sharing**: When a document is shared externally via a link, should the guard treat it as reduced secrecy (the audience has expanded) or maintain the original secrecy (the M365 permission model hasn't changed)?

6. **Draft email**: Should draft emails that haven't been sent be treated as `internal:<tenant>` (author is internal, but content is not finalized) or `published:<tenant>` (it's the user's own draft)?

---

## 10. Relation to Existing Guard Architecture

The Work IQ guard should:

- **Share the same WASM ABI** as the GitHub guard (`label_agent`, `label_resource`, `label_response` with the same memory management protocol).
- **Share the gateway integration** — the MCP Gateway selects the appropriate guard WASM module based on the MCP server being guarded.
- **Use the same DIFC evaluation engine** in the gateway — the gateway's flow-rule enforcement is generic over label sets, so the M365-specific tags are opaque strings to the gateway.
- **Be a separate WASM module** (`workiq-guard-rust.wasm`) with its own label logic, not a mode flag in the GitHub guard.

This means the gateway infrastructure is reusable; only the label derivation logic is M365-specific.
