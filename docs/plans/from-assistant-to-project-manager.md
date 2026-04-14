# From Assistant to Project Manager

## The Problem

The orchestrator today is a strong reactive system: it responds well when asked questions, creates issues competently, delegates work to developers, and keeps Slack threads updated with GitHub events. But it behaves like a skilled assistant waiting for instructions, not like a project manager running the show.

The difference is felt by the Slack user. A PM from an agency doesn't just do what you ask and go quiet. They come back to you when the work is ready to review. They ask smart questions before starting. When you say "the button looks wrong," they know what to do with that feedback without you having to spell out the mechanics. These three gaps -- the feedback loop, proactive prompts, and intake clarification -- are what separate a tool you operate from a person you work with.

This plan addresses all three.

The reference point is specific: a project manager at a software agency who is the client's single point of contact. The client never talks to the developers. They never see a task board, a CI pipeline, or a deployment log. They describe what they want in their own language, the PM handles everything behind the scenes, and the PM comes back when there's something to look at or decide. When the client says "ship it," the PM follows through until the feature is live. The entire internal machinery of the agency is invisible. The client's experience is: I talk to my PM, things happen, I review, I give feedback, and eventually it ships.

Every decision in this plan -- what to say, when to say it, what to surface, what to hide -- is measured against that experience.

---

## The PM Contract

Six principles that govern how the system should behave across all capabilities. They are not features to implement -- they are constraints that every feature must satisfy.

**The user talks to one person.** The PM is the single point of contact. The user never interacts with the developer, the CI system, or the deployment pipeline directly. Information from those systems reaches the user through the PM, translated into language that makes sense to someone who cares about the product, not the process.

**Internal mechanics are invisible.** Issue numbers, PR references, branch names, CI status, deployment logs -- these are the PM's tools, not the user's. The PM may reference an issue number in passing ("I've created #48 for this"), but never expects the user to navigate GitHub, read CI output, or understand what a preview deployment is. If the build is broken, that's the PM's problem to resolve with the developer. The user hears about it only if it affects their timeline.

**The PM owns follow-through.** A conversation that ends with "Will do" and no follow-up is a dropped ball. When the user approves something, the PM confirms when it ships. When the PM relays feedback, the PM comes back with the result. Every thread the PM is part of should reach a resolution the user can see -- not trail off into silence after the last GitHub event.

**The PM speaks the user's language.** "The sidebar is too wide" is a complete instruction. The PM translates it into a developer-actionable comment without asking the user to be more specific, reference a component name, or formalize their feedback. The PM never asks the user to fill out a template, provide acceptance criteria, or speak in technical terms.

**The PM exercises judgment.** Not every decision needs escalation. A competent PM decides whether to ask a clarifying question or just proceed, how to frame vague feedback for the developer, and when a request is clear enough to act on. The user hired the PM to handle things, not to ask permission at every step. When the PM does ask a question, it should be because the answer genuinely changes what gets built.

**The PM is present without being noisy.** The PM messages when the user needs to know something: a preview is ready, work has started, feedback was addressed, a feature is live. Between those moments, silence is professional. But extended silence when the user expects progress is worse than noise -- if something is taking unusually long, the PM says so, briefly, without sounding like an alarm.

---

## 1. The Preview Feedback Loop

### Why This Matters Most

Today the system handles the pipeline from request to working preview well:

```
User asks for feature  -->  Issue created  -->  Developer builds  -->  Preview goes live
```

But after the preview is live, the cycle breaks. The "preview ready" notification arrives in the Slack thread, and then... nothing. The user has to:

1. Notice the notification among other Slack activity
2. Remember to go test the preview
3. Formulate a new @mention to give feedback
4. Understand that their feedback should target the existing issue/PR, not start a new conversation

A real PM would say: "Take a look when you get a chance -- here's the link. Let me know if it matches what you had in mind." And when the user says "the sidebar is too wide," the PM doesn't need to be told *how* to relay that. They just do it.

### What Needs to Change

**After a preview goes live, the bot should prompt for review.** Not just "preview ready" as a status line. An actual call to action: "This is ready for you to look at. Try it out and tell me what you think." This message should include:
- The preview URL
- The user-facing note if one exists (test credentials, etc.)
- A reminder of what was originally requested (the issue title/description, briefly)

The user should be able to respond naturally in the thread: "Looks good but the colors are off" or "The login page crashes when I leave the email empty." The agent should:
- Recognize this as feedback on the current preview
- Translate it into an actionable developer instruction
- Delegate it to the developer on the existing issue/PR (not create a new issue)
- Acknowledge to the user: "Got it, I've passed that along."

**When the developer pushes again and a new preview deploys, the bot should close the loop.** Not just "preview ready" again, but something that acknowledges the context: "Updated preview is live -- this should address the feedback you gave." This invites the user into another review round naturally.

This creates the full value cycle:

```
Request --> Build --> Preview --> Review Prompt --> User Feedback --> Relay to Dev
  ^                                                                       |
  |_______________________________________________________________________| 
                            (iterate until done)
```

### The Nuance

Feedback relay is not just "post a comment." The agent needs to distinguish between:

- **Actionable feedback** ("the button should be blue, not green") -- relay to developer via `/opencode`
- **Questions** ("why does this page load slowly?") -- investigate or discuss, don't delegate
- **Approval** ("looks great, ship it") -- acknowledge, initiate the merge, and follow up when it's live. The conversation doesn't end at "Will do." It ends at "This is live now."

The agent already has a multi-specialist router that classifies intent. This is an extension of that capability: within the context of a preview thread, recognize what kind of response the user is giving and route accordingly.

### What This Achieves

The user's experience shifts from "I have to manage the tool" to "the tool manages the process." They stay in Slack, speak naturally, and the system handles the mechanics of GitHub comments, developer delegation, and preview tracking.

---

## 2. Proactive Outreach for Moments That Need Attention

### Why This Matters

The current system posts status updates reactively: things happened, here's what changed. These updates are useful but easy to miss, because they look the same whether or not the user needs to do something. The Slack thread becomes a log rather than a conversation.

A PM distinguishes between "FYI, this happened" and "I need something from you." The system should too.

### What Needs to Change

Proactive outreach should be limited to moments where the user genuinely needs to act. Specifically:

**a) "Your preview is ready for review"**

This overlaps with the feedback loop above. When a preview goes live, the message should be more than a status update. It should be framed as a request for the user's attention. This is especially important for the *first* preview on an issue (the initial implementation), where the user may not be watching the thread closely.

**b) "The developer submitted work for your request"**

When a PR opens that's linked to an issue the user created via the bot, the bot should connect the dots. Not just "PR #52 opened" as a generic event, but: "A pull request is up for the feature you asked about. There's a preview building -- I'll let you know when it's ready to look at."

This tells the user: work is happening, and you'll be prompted when it's your turn again. It sets expectations without requiring action.

**c) "Updated preview reflects your feedback"**

When the user gave feedback, the agent relayed it, the developer pushed again, and a new preview deployed -- this isn't just "preview ready (again)." It's "the developer addressed what you mentioned -- take another look." This frames the notification as a continuation of the conversation, not a disconnected event.

### What This Does NOT Include

Proactive outreach should not nag or create noise:

- No "this issue has been idle for N days." The user knows what they asked for and doesn't need the bot pestering them about it. If something is stuck, they'll ask.
- No "CI has been failing for hours." CI failure notifications already post to the thread. Escalating them into proactive nudges turns the bot into an alarm system, which is the opposite of the PM feel.
- No unsolicited weekly summaries or status digests. If the user wants a summary, they can ask the researcher.

There is one exception to the "don't nag" rule: **when silence itself becomes confusing.** If a workstream has been in `in-progress` for significantly longer than usual with no preview ever going live, the user will notice the quiet and wonder what happened. A PM in that situation wouldn't wait to be asked -- they'd say: "This is taking longer than expected -- I'm checking with the developer." This isn't a status nag; it's expectation management. The trigger should be the absence of an expected event (no preview after a reasonable window), not the presence of a routine one (CI failed and retried).

The test for whether a proactive message belongs: **does the user need to do something that they might not know they need to do?** "Preview is ready for you to review" passes this test. "Your issue is idle" does not -- the user already knows.

### The Framing Matters

The difference between a status update and a proactive prompt is mostly about language and context:

| Status update (current) | Proactive prompt (new) |
|---|---|
| "The preview is live" | "This is ready for you to try out. Let me know what you think." |
| "PR #52 opened by @dev" | "Work has started on your request. I'll let you know when there's something to look at." |
| "Preview updated" | "Updated preview is live -- this should reflect the changes you asked for." |
| "PR merged" | "This is live now. Let me know if anything looks off in production." |

Same events, different framing. The status update informs. The proactive prompt invites action and sets the user's expectation of what happens next. The PM never leaves the user wondering "so... is it done?"

---

## 3. Intake Clarification Before Action

### Why This Matters

Today, when a user says "@bot add dark mode support," the agent immediately creates a GitHub issue. This is fast, but it often produces a vague issue that the developer then has to interpret or ask about. A real PM would pause:

"Dark mode for the whole app or just the settings page? Anything specific about the color scheme, or should the developer pick something reasonable?"

One or two focused questions dramatically improve the quality of the resulting issue and save a round-trip with the developer later.

### What Needs to Change

**The agent should assess whether a request is clear enough to act on.** Some requests are specific enough to proceed immediately:

- "The login page returns a 500 error when the email field contains a plus sign" -- this is a clear bug report, no clarification needed.
- "Add a CSV export button to the customer list page, below the search bar" -- specific enough.

Others are vague and would benefit from a brief interview:

- "Add dark mode" -- scope unclear.
- "Improve the performance" -- which page? What's slow?
- "Add analytics" -- what kind? What events? What tool?

**The agent should ask focused, minimal questions.** Not a requirements document. One or two questions that would meaningfully change what gets written in the issue. The goal is to sound like a PM quickly scoping work, not an intake form.

Good: "Dark mode for the whole app, or a specific section? And should it follow the OS setting or be a manual toggle?"

Bad: "Please provide acceptance criteria, priority level, affected components, and estimated timeline."

**After clarification, the agent creates the issue with all gathered context.** The issue body should reflect the conversation naturally -- not just dump the Q&A transcript, but synthesize it into a coherent description.

### The Judgment Call

Not every request needs clarification. The agent should lean toward acting when the request is reasonably specific, and toward asking when the request is genuinely ambiguous. Erring too far toward always asking makes the bot feel slow and bureaucratic. Erring too far toward never asking produces garbage issues.

The heuristic: **if a competent developer would need to ask a clarifying question before starting, the bot should ask first.** If a developer could reasonably start working with what's given, the bot should too.

This also means the clarification should be calibrated to the type of request:
- **Bug reports**: usually need less clarification (reproduction steps, expected vs actual)
- **Feature requests**: often need scoping (what exactly, where in the app, any constraints)
- **Improvement requests**: almost always vague ("make it faster," "improve the UX")

### Conversation Flow

The intake conversation should feel natural in Slack:

```
User:    @bot add dark mode support
Bot:     Before I create a ticket for this -- should dark mode cover the entire app 
         or just a specific section? And should it follow the OS preference 
         automatically or be a manual toggle?
User:    Whole app, and follow the OS setting.
Bot:     Got it. I've created #48: "Add system-wide dark mode following OS preference."
         I'll delegate this to the developer.
```

Two turns, not five. The bot doesn't ask about priority, timeline, or technical approach -- that's the developer's domain.

### What to Avoid

- Asking multiple questions in rapid succession. One message with one or two questions is enough.
- Asking about things the developer will decide anyway (architecture, library choices, implementation approach). The user hired the agency to make those calls.
- Refusing to proceed without answers. If the user says "just create it, I'll add details later," the bot should comply gracefully -- a PM who insists on a complete brief before lifting a finger doesn't last long.
- Over-qualifying. "Would you like me to create an issue?" is unnecessary -- the user already asked for the feature. The bot is deciding *how* to create the issue, not *whether* to. A PM doesn't ask for permission to do PM work.

---

## How the Three Capabilities Reinforce Each Other

These aren't independent features. Together they transform the interaction model:

**Without all three**, the conversation pattern is:
```
User: @bot add dark mode            -->  (vague issue created)
...radio silence...
User: @bot what's the status?       -->  "PR is open, preview is live"
User: @bot the colors are wrong     -->  (user has to know to say this here)
...radio silence...
```

**With all three**, it becomes:
```
User: @bot add dark mode
Bot:  Whole app or specific section? OS preference or manual toggle?
User: Whole app, OS preference
Bot:  Created #48. I'll get this to the developer.

  ...time passes...

Bot:  Work is underway on your dark mode request.

  ...time passes...

Bot:  The preview is ready -- try it out and let me know what you think.
      https://preview-48.example.com
      (Sign in with test@example.com / test1234)
User: Looks good but the contrast on the sidebar is too low
Bot:  Noted, I've passed that to the developer.

  ...time passes...

Bot:  Updated preview is live -- the sidebar contrast should be improved.
User: Perfect, ship it
Bot:  On it.

  ...time passes...

Bot:  Dark mode is live. Let me know if anything looks off in production.
```

This reads like a conversation with a PM from an agency. The user never had to think about GitHub issues, PR numbers, CI pipelines, or deployment mechanics. They never had to ask "what's the status?" or "is this done?" They described what they wanted, answered one scoping question, reviewed a preview, gave feedback, approved, and were told when it shipped. The PM handled everything between those moments -- and the user doesn't know or care what "everything" involved.

---

## 4. Building the Right Context: The Workstream Picture

### Why This Section Exists

The three capabilities above all depend on the same underlying problem: when the agent fires -- whether from a Slack @mention or a GitHub webhook -- it needs to understand the ongoing workstream the way a human PM would.

A PM walking into a thread doesn't just see the last message. They remember: the user asked for dark mode three days ago. I created issue #48. A developer picked it up and submitted a PR. The first preview had a contrast problem that the user flagged. The developer pushed a fix. Now a new preview is live. The next thing I should do is tell the user to take another look.

Today the system reconstructs a partial version of this picture at invocation time, but it's fragmented. The pieces exist in different places and some of them never reach the agent at all. Getting this right is what makes the three capabilities above actually work -- without it, the feedback loop doesn't know it's a feedback loop, the proactive message doesn't know what to be proactive about, and the intake flow doesn't know it's mid-conversation.

### What the Agent Knows Today (and Where It Comes From)

When a Slack @mention triggers the agent, context is assembled from multiple sources:

**The router gets a thin slice:**
- The user's text (bot mention stripped)
- If the thread is mapped to an issue: just the number, title, and state
- The last 5 Slack thread messages, each truncated to 200 characters, capped at 2000 characters total

This is enough for routing but nothing more. The router can tell "this is about issue #48" but not "the user already gave feedback on the preview and this is a follow-up."

**Each specialist gets a fuller picture, built by `BuildContext` with a token budget:**
- System prompt (always, untruncated)
- User message (always, reserved upfront)
- Linked issue body (truncated to ~4000 characters / 1000 tokens)
- Up to 20 thread messages from Slack (oldest dropped first under budget pressure)
- A feature status summary: one-line snapshots of issue state, PR state, CI status, preview status
- Prior step context if specialists are chained

**The feature summary** comes from `FeatureSnapshot`, assembled by fetching from GitHub and the preview database:
```
Issue: #48 Add dark mode (open)
PR: #52 by developer — open, +150/-30 lines, branch feature/dark-mode
CI: build passing, lint passing
Preview: ready at https://preview-52.example.com
```

**When a GitHub webhook fires (not a user mention)**, the notifier assembles a `FeatureSnapshot` for message generation, but no agent is invoked. The `MessageGenerator` formats a status message from the snapshot and posts it to the Slack thread.

### What's Missing

The current context is a **point-in-time snapshot**. It captures the state of each artifact (issue, PR, CI, preview) right now, but not the history of what happened across them. The agent doesn't know:

**a) The lifecycle phase of the workstream.**

Is this a brand-new request where no issue exists yet? An issue waiting for a developer to pick it up? A PR under active development? A preview that's been deployed and is awaiting the user's review? A revision cycle after the user gave feedback? The agent today can infer some of this from the feature summary (if a PR exists, if a preview is ready), but it's reconstructing the narrative from artifacts rather than knowing it directly.

This matters because the same user message means different things at different phases. "Looks good" during intake means "proceed with the issue." After a preview, it means "I approve this, ship it." The router needs to know the phase to route correctly, and the specialist needs to know it to respond appropriately.

**b) Whether the user has seen and responded to the current preview.**

When a preview goes live, the notifier posts a message. If the user then says something in the thread, is that feedback on the preview or an unrelated follow-up? Today there's no way to tell. The agent would need to know: (1) a preview notification was posted, (2) the user has or hasn't responded since then.

**c) What feedback was previously given and relayed.**

If the user said "the contrast is too low" and the agent relayed that to the developer, and now a new preview is live -- the notifier should frame this as "updated preview reflecting your feedback" rather than a generic "preview ready." But the notifier doesn't know that feedback was given because that happened in the agent's domain, and the notification pipeline is separate.

**d) What the agent itself previously did in this thread.**

The agent's own actions -- issues created, comments posted, delegations made -- are tracked in `SideEffects` during a single invocation but not persisted across invocations. The only record is in `agent_traces` (which auto-deletes after 24 hours) and in the Slack thread messages themselves (which the agent reads back, but only within token budget).

### The Workstream as a First-Class Concept

The three capabilities require a shared understanding of "where things stand" that goes beyond the current artifact-level snapshots. What's needed is a workstream-level context that captures:

1. **Phase**: What lifecycle stage is this workstream in?
2. **Origin**: Was this initiated by the user through Slack? Which thread?
3. **Key events**: What has happened, in what order? (Not every GitHub event -- just the ones that matter for the narrative.)
4. **Pending action**: Whose turn is it? Is the system waiting for user feedback? Waiting for the developer? Waiting for CI?

This doesn't need to be a complex workflow engine. It can be a lightweight record that gets updated when significant things happen, and that's read whenever context is assembled.

### Lifecycle Phases

A workstream goes through a recognizable sequence. Not all steps occur for every request, and the sequence can loop, but the phases are:

```
intake          The user expressed a request. No issue exists yet. The agent may be 
                asking clarifying questions.

open            An issue has been created. No one is working on it yet. The system 
                may have delegated it, but no PR exists.

in-progress     A PR exists (linked to the issue). The developer is working.
                Previews may be building or failing.

review          A preview is live and the user hasn't given feedback yet.
                This is the "your turn" state for the user.

revision        The user gave feedback. The agent relayed it. The developer is 
                expected to push an update. Similar to in-progress but with the 
                knowledge that this is a feedback-driven iteration.

done            The PR is merged and the feature is live. The PM has confirmed 
                deployment to the user. This is the user's "done," not the 
                developer's -- the workstream isn't complete until the user 
                knows the work shipped.

abandoned       The user explicitly cancelled the request ("never mind," "let's not 
                do this") or the PM closed a stale workstream. The PM acknowledges 
                cleanly: "Got it, I've shelved this."
```

The phase is determined by the state of the artifacts plus a few key events:
- Issue created → `open`
- PR opened → `in-progress`
- Preview becomes ready → `review`
- User responds after preview-ready notification → back to `revision` (if feedback) or `done` (if approval, after merge confirms)
- New push after feedback → `in-progress` (briefly), then `review` again when preview is ready
- PR merged → `done` (after the PM confirms deployment to the user)
- User cancels or abandons → `abandoned`

Note on scope changes: when the user pivots mid-stream ("actually, make it a toggle instead of following OS preference"), this is not a new workstream. It's feedback that changes the goal, not just the implementation. The PM acknowledges the change, updates the issue, and relays to the developer -- staying in `revision`. The workstream's identity follows the user's intent, not the technical artifacts.

### Where Phase Tracking Lives

The `previews` table already tracks preview state per PR. The `slack_threads` table already maps threads to issues and PRs. The missing piece is a small amount of workstream-level state:

- **Current phase** (the values above)
- **Preview notification posted at** (timestamp of the last "preview ready" message in the thread -- needed to distinguish "user responded to preview" from "user said something unrelated before the preview was ready")
- **Feedback relayed** (flag or timestamp -- did the agent relay user feedback since the last preview notification?)

This could live on the existing `slack_threads` record (it's scoped to one thread, one issue/PR pair -- which is the workstream) or as a separate lightweight table. The point is: it's a few fields, not a workflow engine.

### How Workstream Context Flows into the Agent

Today, context assembly happens in two places: the Slack handler builds a `RunRequest`, and `BuildContext` arranges it for the LLM. The workstream context should enter at the `RunRequest` level and flow through to both the router and the specialists.

**What changes for the router:**

The router currently gets a thin summary: user text, issue number, and 5 truncated thread messages. This is where most routing mistakes will happen for the new capabilities -- the router can't distinguish "user giving feedback on preview" from "user making a new request" without knowing the phase.

The router needs one additional signal: the workstream phase. This is a single word. It doesn't need to be in the thread summary or reconstructed from artifacts. It should be stated explicitly:

```
[Workstream phase: review — a preview is live and waiting for the user's feedback]
```

This is cheap (under 20 tokens) and directly actionable for routing. If the phase is `review` and the user says "the sidebar is too wide," the router knows to send this to the delegator as feedback relay, not to the issue_creator as a new request. If the phase is `intake` and the user responds to a clarifying question, the router knows to continue with issue creation.

**What changes for specialists:**

The feature summary already gives specialists a snapshot of artifact states. The workstream phase adds the narrative layer. But more importantly, certain specialists need phase-specific behavior:

- The **delegator**, when the phase is `review` or `revision`, should frame its `/opencode` comment as feedback relay, not fresh delegation. It should reference what the user said and connect it to the existing PR, not write a standalone instruction.

- The **issue_creator**, when the phase is `intake`, should check whether the prior bot message in the thread was a clarifying question. If so, the user's response is an answer to that question, and the specialist should synthesize the full conversation into an issue -- not ask again or treat the response as a separate request.

- The **researcher**, when the phase is `review`, should know that the user might be asking questions about the preview ("why is this page slow?") and should have the preview URL and PR context at hand.

These are prompt-level changes in the specialist templates, conditioned on the phase. The phase is just a string that gets interpolated into the system prompt alongside repo owner/name.

### How Workstream Context Flows into Notifications

The notification pipeline (GitHub webhook → notifier → message generator → Slack) currently produces uniform messages regardless of workstream state. This is where proactive outreach lives.

When the notifier processes a `preview_ready` event, it currently formats it as a status update. With workstream context, it can:

1. Look up the workstream phase for this PR's thread.
2. If the phase is `in-progress` (first preview): frame as a review prompt. "This is ready for you to try out. Let me know what you think."
3. If the phase is `revision` (user previously gave feedback): frame as a follow-up. "Updated preview is live -- this should address the feedback you gave."
4. Update the phase to `review`.

Similarly, when a PR opens and the notifier sees that the thread's phase is `open` (issue exists, no PR yet): frame as progress update. "Work has started on your request. I'll let you know when there's something to look at." Update the phase to `in-progress`.

The `MessageGenerator` already takes a `FeatureSnapshot`. It would additionally take the workstream phase (or the phase could be a field on the snapshot). The phase determines tone and framing, not content -- the same underlying data (PR number, preview URL, CI status) is presented differently depending on where the workstream is.

### Phase Transitions as Explicit Events

Today, state changes happen implicitly when artifacts change. Preview goes from "building" to "ready" -- that's a preview-level state change. The workstream phase is a higher-level concept that should be updated explicitly at well-defined points:

| Trigger | Phase transition | Where it happens |
|---|---|---|
| Agent creates issue in thread | → `open` | Agent tool callback (`OnIssueCreated`) |
| PR opened, linked to issue | → `in-progress` | GitHub webhook handler |
| Preview becomes ready | → `review` | Preview service, after health check passes |
| User @mentions bot while phase is `review` | → `revision` (if feedback) or stays `review` (if question) | Slack handler, after router classifies intent |
| User approves ("ship it", "looks good") | → `done` (pending merge) | Agent, after router classifies as approval |
| PR merged + deployment confirmed | → `done` | GitHub webhook handler, after PM posts "this is live" |
| PR closed without merge | → `open` (back to issue) | GitHub webhook handler |
| User cancels ("never mind", "scrap this") | → `abandoned` | Agent, after router classifies as cancellation |
| User pivots scope ("actually, make it X") | stays in current phase | Agent relays updated scope to developer |

Each transition is a single field update on the workstream record. The transitions are driven by events that already flow through the system -- no new event sources needed, just a write at the right moment.

### What Does NOT Need to Be Stored

It's tempting to build a full audit trail of the workstream: every event, every message, every state change with timestamps. This is unnecessary and would add complexity without value.

The agent doesn't need to know the full history -- it needs to know the current phase and a few key facts:
- Is there a preview live right now?
- Did the user give feedback that was relayed?
- Was the last bot message a clarifying question (for intake)?

Everything else can be derived from the existing sources: the Slack thread messages (which the agent already reads), the GitHub API (which the agent already queries via tools), and the preview database.

The workstream record is small: phase, a couple of timestamps, and maybe a flag or two. It's a coordination primitive, not a data store.

### Token Budget Implications

The workstream phase itself is nearly free -- a single line like `[Workstream phase: review]` costs under 20 tokens. But the three capabilities introduce new context that competes for the existing budget:

- **Intake clarification** needs the thread history to include the bot's clarifying question and the user's answer. This is already covered by the 20-message thread history, as long as the conversation is recent. No budget change needed.

- **Feedback relay** needs the user's feedback text to be preserved in full, because it will be paraphrased into a developer instruction. If the feedback is in the most recent thread message, it's already in the user text field (which is always included). If it's a few messages back, it's in the thread history. The current 20-message cap and 8000-token budget should be sufficient for typical feedback conversations.

- **Proactive outreach** doesn't involve the agent at all (it's notification-side), so it has no token impact.

The one area where budget pressure might increase: if the specialist needs to reference both the original request (from the issue body, up to 1000 tokens) and the recent feedback conversation (from thread history), that's more context than a simple one-shot request. The current budget should handle this, but it's worth monitoring. If the issue body is long and the thread has many messages, the oldest thread messages will be dropped first -- which is the right behavior, since recent feedback matters more than early conversation.

### Interaction Between Context and Routing

The router is the most critical consumer of workstream context because routing mistakes compound. If the router misclassifies feedback as a new feature request, the issue_creator will create a duplicate issue. If it misclassifies a clarification answer as a standalone request, the intake conversation breaks.

Today the router makes its decision from:
- User text
- Issue number/title/state
- 5 thread messages (heavily truncated)

Adding the workstream phase gives the router the single most important signal it currently lacks. Consider two identical user messages:

> "Make the sidebar narrower"

- Phase `review`: This is feedback on the live preview → route to delegator for feedback relay
- Phase `open` (no PR yet): This is a new request or refinement of the issue → route to commenter or delegator
- No phase (top-level mention, new thread): This is a brand-new request → route to issue_creator

The router prompt should include the phase as an explicit routing signal, with guidance on how it affects classification. This is a prompt change, not an architectural one.

### Putting It Together: Context at Each Trigger Point

**When a user @mentions the bot in Slack:**

1. Slack handler looks up thread → workstream record (phase, linked issue/PR)
2. Feature assembler builds `FeatureSnapshot` from GitHub + preview DB (as today)
3. `FormatFeatureSummary` includes the phase as a heading
4. `RunRequest` carries: user text, thread messages, linked issue, feature summary with phase, workstream phase separately for the router
5. Router uses phase + user text + thread summary to route
6. Specialist uses phase + full context to respond appropriately
7. After the agent responds, the handler updates the workstream phase if needed (e.g., feedback given → `revision`)

**When a GitHub webhook fires (no user mention):**

1. Notifier receives event (PR opened, preview ready, CI failed, etc.)
2. Notifier looks up thread → workstream record
3. Feature assembler builds `FeatureSnapshot` (as today)
4. `MessageGenerator` uses phase + snapshot to determine framing (status update vs. proactive prompt)
5. Notifier posts to Slack thread
6. Notifier updates workstream phase if the event triggers a transition (PR opened → `in-progress`, preview ready → `review`)

The key insight: **both paths -- user-triggered and event-triggered -- read and write the same workstream record.** This is what makes the interaction feel continuous. When the user gives feedback, the agent updates the phase to `revision`. When the developer pushes and a new preview goes live, the notifier sees `revision` and knows to frame the message as "updated preview reflecting your feedback." The two paths share state through the workstream record, which is what makes the experience feel like a single ongoing conversation rather than disconnected reactions.

---

## Sequencing

The workstream context (section 4) is the foundation. It should be built first because everything else depends on it. Without the phase signal, the router can't distinguish feedback from new requests, the notifier can't frame messages as proactive prompts, and the intake flow can't track that it's mid-conversation. The workstream record itself is small -- a phase field and a couple of timestamps on the existing thread mapping -- but wiring it into both the agent path and the notification path is the structural change that enables all three capabilities.

With the workstream context in place, the three capabilities have a natural order:

1. **Intake clarification** comes first because it improves issue quality at the source. Every other capability downstream benefits from better-scoped issues. It's also the most contained change -- it affects the issue_creator specialist and the router, without touching notifications or the preview pipeline. The workstream phase (`intake` → `open`) provides the signal the router needs to know that a user response is an answer to a clarifying question, not a new request.

2. **The feedback loop** comes second because it's the highest-impact change for the user experience. It closes the cycle from preview to iteration, which is where the current system most noticeably drops the ball. It requires changes to how the agent interprets responses in preview threads and how it delegates feedback, so it touches more surface area. The workstream phase (`review` → `revision` → `review`) is what makes the loop recognizable as a loop rather than a series of unconnected interactions.

3. **Proactive outreach** comes last because it builds on both of the above. The most valuable proactive message ("preview is ready, please review") only works well if the feedback loop is in place to handle what comes next. And the "work has started" message is more useful when the original issue was well-scoped from the intake conversation. The workstream phase is what tells the notifier to frame a `preview_ready` event as a review prompt when the phase is `in-progress`, or as "updated preview reflecting your feedback" when the phase is `revision`.

Each capability is independently valuable and can be shipped incrementally, but the full PM experience emerges when all three work together -- and the workstream context is what ties them into a coherent whole.

The test for whether the system has arrived is not technical. It's experiential. A user who has been working with the bot for a few weeks should, if asked how their development process works, say something like: "I tell my PM what I need, they handle it, and they come back to me when there's something to look at." If they say "I use a Slack bot that creates GitHub issues," the system is still an assistant.
