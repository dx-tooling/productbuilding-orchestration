package domain

import (
	"context"
	"fmt"
	"log/slog"
)

// maxOrchestratorSteps caps the number of specialist steps per request
// to prevent pathological routing from causing excessive LLM calls.
const maxOrchestratorSteps = 5

// Orchestrator implements AgentRunner by routing to specialized agents.
type Orchestrator struct {
	router             *Router
	specialists        map[string]*Specialist
	llm                LLMClient
	tools              ToolExecutor
	model              string
	slackFetcher       SlackThreadFetcher
	conversationLister ConversationLister
	workspace          string
	tokenBudget        TokenBudget
}

// OrchestratorConfig holds optional configuration for the Orchestrator.
type OrchestratorConfig struct {
	ConversationLister ConversationLister
	Workspace          string
	TokenBudget        TokenBudget
}

// NewOrchestrator creates a new multi-agent orchestrator.
func NewOrchestrator(llm LLMClient, tools ToolExecutor, slackFetcher SlackThreadFetcher, model string, cfg OrchestratorConfig) *Orchestrator {
	budget := cfg.TokenBudget
	if budget.Total == 0 {
		budget = DefaultTokenBudget()
	}

	o := &Orchestrator{
		router:             NewRouter(llm, model),
		llm:                llm,
		tools:              tools,
		model:              model,
		slackFetcher:       slackFetcher,
		conversationLister: cfg.ConversationLister,
		workspace:          cfg.Workspace,
		tokenBudget:        budget,
	}

	o.specialists = o.buildSpecialists()
	return o
}

// buildSpecialists creates all specialist instances.
func (o *Orchestrator) buildSpecialists() map[string]*Specialist {
	specs := map[string]SpecialistConfig{
		"issue_creator": {
			Name:           "issue_creator",
			PromptTemplate: issueCreatorPromptTmpl,
			ToolDefs:       IssueCreatorTools(),
			MaxIterations:  4,
		},
		"delegator": {
			Name:           "delegator",
			PromptTemplate: delegatorPromptTmpl,
			ToolDefs:       DelegatorTools(),
			MaxIterations:  3,
		},
		"commenter": {
			Name:           "commenter",
			PromptTemplate: commenterPromptTmpl,
			ToolDefs:       CommenterTools(),
			MaxIterations:  3,
		},
		"researcher": {
			Name:           "researcher",
			PromptTemplate: researcherPromptTmpl,
			ToolDefs:       ResearcherTools(),
			MaxIterations:  5,
		},
		"closer": {
			Name:           "closer",
			PromptTemplate: closerPromptTmpl,
			ToolDefs:       CloserTools(),
			MaxIterations:  3,
		},
	}

	result := make(map[string]*Specialist, len(specs))
	for name, cfg := range specs {
		result[name] = &Specialist{
			config:             cfg,
			llm:                o.llm,
			tools:              o.tools,
			model:              o.model,
			slackFetcher:       o.slackFetcher,
			conversationLister: o.conversationLister,
			workspace:          o.workspace,
			tokenBudget:        o.tokenBudget,
		}
	}
	return result
}

// Run implements the AgentRunner interface.
func (o *Orchestrator) Run(ctx context.Context, req RunRequest) (RunResponse, error) {
	slog.Info("orchestrator: routing request",
		"channel", req.ChannelID,
		"user", req.UserName,
	)

	// Pre-fetch thread context once (for router AND specialists)
	if req.ThreadTs != "" && req.ThreadMessages == nil && o.slackFetcher != nil {
		fetched, fetchErr := o.slackFetcher.GetThreadReplies(ctx, req.Target.SlackBotToken, req.ChannelID, req.ThreadTs)
		if fetchErr != nil {
			slog.Warn("orchestrator: failed to fetch thread replies", "error", fetchErr)
		} else {
			req.ThreadMessages = fetched
		}
	}

	// Route (now with thread context)
	decision, err := o.router.Route(ctx, req.UserText, req.Target, req.LinkedIssue, req.ThreadMessages)
	if err != nil {
		return RunResponse{}, fmt.Errorf("orchestrator routing: %w", err)
	}

	slog.Info("orchestrator: routing decision",
		"steps", len(decision.Steps),
		"channel", req.ChannelID,
	)

	// Execute specialists in sequence
	var mergedEffects SideEffects
	var lastText string
	var prior *PriorStepContext

	steps := decision.Steps
	if len(steps) > maxOrchestratorSteps {
		slog.Warn("orchestrator: truncating excessive routing steps",
			"requested", len(steps),
			"max", maxOrchestratorSteps,
		)
		steps = steps[:maxOrchestratorSteps]
	}

	for i, step := range steps {
		specialist, ok := o.specialists[step.Specialist]
		if !ok {
			slog.Warn("orchestrator: unknown specialist, falling back to researcher",
				"specialist", step.Specialist,
			)
			specialist = o.specialists["researcher"]
		}

		slog.Info("orchestrator: executing specialist",
			"step", i+1,
			"specialist", specialist.config.Name,
			"channel", req.ChannelID,
		)

		// If a previous step created an issue, inject it as linked issue for the next step
		stepReq := req
		if prior != nil && len(prior.Effects.CreatedIssues) > 0 {
			created := prior.Effects.CreatedIssues[0]
			stepReq.LinkedIssue = &IssueContext{
				Number: created.Number,
				Title:  created.Title,
			}
		}

		result, err := specialist.Run(ctx, stepReq, prior)
		if err != nil {
			return RunResponse{}, fmt.Errorf("specialist %s: %w", specialist.config.Name, err)
		}

		// Handle reroute: if the specialist signals a different specialist should handle this
		if result.Reroute != "" {
			if rerouteSpec, ok := o.specialists[result.Reroute]; ok {
				slog.Info("orchestrator: rerouting from specialist",
					"from", specialist.config.Name,
					"to", result.Reroute,
					"channel", req.ChannelID,
				)
				result, err = rerouteSpec.Run(ctx, stepReq, nil)
				if err != nil {
					return RunResponse{}, fmt.Errorf("rerouted specialist %s: %w", result.Reroute, err)
				}
			} else {
				slog.Warn("orchestrator: reroute target not found, keeping original result",
					"target", result.Reroute,
				)
			}
		}

		// Merge side effects
		mergedEffects.CreatedIssues = append(mergedEffects.CreatedIssues, result.SideEffects.CreatedIssues...)
		mergedEffects.PostedComments = append(mergedEffects.PostedComments, result.SideEffects.PostedComments...)
		mergedEffects.DelegatedIssues = append(mergedEffects.DelegatedIssues, result.SideEffects.DelegatedIssues...)

		lastText = result.Text
		prior = &PriorStepContext{
			StepName:   specialist.config.Name,
			ResultText: result.Text,
			Effects:    result.SideEffects,
		}
	}

	return RunResponse{
		Text:        lastText,
		SideEffects: mergedEffects,
	}, nil
}
