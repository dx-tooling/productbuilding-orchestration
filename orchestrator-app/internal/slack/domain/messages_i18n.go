package domain

// MessageKey identifies a translatable string.
type MessageKey string

const (
	// message_generator.go — parent messages
	msgOpenedBy MessageKey = "opened_by"
	msgOpenedPR MessageKey = "opened_pr"
	msgTouching MessageKey = "touching"
	msgViewPR   MessageKey = "view_pr"
	msgGitHub   MessageKey = "github_lnk"

	// message_generator.go — event messages
	msgWorkStarted     MessageKey = "work_started"
	msgPreviewLive     MessageKey = "preview_live"
	msgPreviewReview   MessageKey = "preview_review"
	msgPreviewRevision MessageKey = "preview_revision"
	msgPreviewFailed   MessageKey = "preview_failed"
	msgLogsForDetails  MessageKey = "logs_for_details"
	msgAskInvestigate  MessageKey = "ask_investigate"
	msgCommentedOnGH   MessageKey = "commented_on_gh"
	msgViewComment     MessageKey = "view_comment"
	msgPRMergedReview  MessageKey = "pr_merged_review"
	msgPRMerged        MessageKey = "pr_merged"
	msgPRMergedCI      MessageKey = "pr_merged_ci"
	msgPreviewTeardown MessageKey = "preview_teardown"
	msgCIFailed        MessageKey = "ci_failed"
	msgCIFailedJob     MessageKey = "ci_failed_job"
	msgViewRun         MessageKey = "view_run"
	msgOpenPreview     MessageKey = "open_preview"
	msgLogs            MessageKey = "logs"
	msgNote            MessageKey = "note"
	msgUpdate          MessageKey = "update"

	// handlers.go — error messages (exported for use from web package)
	MsgErrUnavailable MessageKey = "err_unavailable"
	MsgErrOverloaded  MessageKey = "err_overloaded"
	MsgErrRateLimit   MessageKey = "err_rate_limit"
	MsgErrGeneric     MessageKey = "err_generic"

	// handlers.go — fallback messages (exported for use from web package)
	MsgFallbackCreated   MessageKey = "fallback_created"
	MsgFallbackDelegated MessageKey = "fallback_delegated"
)

var translations = map[string]map[MessageKey]string{
	"en": {
		msgOpenedBy:        "Opened by @%s",
		msgOpenedPR:        "@%s opened a pull request%s",
		msgTouching:        ", touching %d lines",
		msgViewPR:          "View PR",
		msgGitHub:          "GitHub",
		msgWorkStarted:     "Work has started on your request. I'll let you know when there's something to look at.",
		msgPreviewLive:     "The preview is live — you can try it out here:",
		msgPreviewReview:   "This is ready for you to try out. Let me know what you think.",
		msgPreviewRevision: "Updated preview is live — this should address the feedback you gave.",
		msgPreviewFailed:   "The preview failed during the %s step.",
		msgLogsForDetails:  " Check the <%s|logs> for details, or ask me to investigate.",
		msgAskInvestigate:  " Ask me to investigate if you'd like.",
		msgCommentedOnGH:   "@%s commented on GitHub:",
		msgViewComment:     "View comment",
		msgPRMergedReview:  "This is live now. Let me know if anything looks off in production.",
		msgPRMerged:        "This PR has been merged.",
		msgPRMergedCI:      " CI was passing on the final commit.",
		msgPreviewTeardown: " The preview will be torn down shortly.",
		msgCIFailed:        "CI failed on the latest push.",
		msgCIFailedJob:     "CI failed on the latest push. The `%s` job failed:",
		msgViewRun:         "View run",
		msgOpenPreview:     "Open Preview",
		msgLogs:            "Logs",
		msgNote:            "Note:",
		msgUpdate:          "Update: %s",

		MsgErrUnavailable: "The AI service is temporarily unavailable. Please try again in a few minutes.",
		MsgErrOverloaded:  "The AI service is currently overloaded. Please try again in a few minutes.",
		MsgErrRateLimit:   "The AI service is rate-limited right now. Please try again shortly.",
		MsgErrGeneric:     "Sorry, I encountered an error processing your request. Please try again.",

		MsgFallbackCreated:   "Created <%s|#%d>: %s",
		MsgFallbackDelegated: "Delegated to %s",
	},
	"de": {
		msgOpenedBy:        "Erstellt von @%s",
		msgOpenedPR:        "@%s hat einen Pull Request eröffnet%s",
		msgTouching:        ", %d Zeilen betroffen",
		msgViewPR:          "PR ansehen",
		msgGitHub:          "GitHub",
		msgWorkStarted:     "Die Arbeit an deiner Anfrage hat begonnen. Ich melde mich, sobald es etwas zum Anschauen gibt.",
		msgPreviewLive:     "Die Vorschau ist bereit — du kannst sie hier ausprobieren:",
		msgPreviewReview:   "Das ist fertig zum Testen. Sag mir, was du davon hältst.",
		msgPreviewRevision: "Die aktualisierte Vorschau ist bereit — das sollte dein Feedback berücksichtigen.",
		msgPreviewFailed:   "Die Vorschau ist beim Schritt %s fehlgeschlagen.",
		msgLogsForDetails:  " Schau in die <%s|Logs> für Details, oder frag mich, ob ich nachforschen soll.",
		msgAskInvestigate:  " Frag mich, wenn ich nachforschen soll.",
		msgCommentedOnGH:   "@%s hat auf GitHub kommentiert:",
		msgViewComment:     "Kommentar ansehen",
		msgPRMergedReview:  "Das ist jetzt live. Sag mir, falls in der Produktion etwas auffällt.",
		msgPRMerged:        "Dieser PR wurde gemergt.",
		msgPRMergedCI:      " CI war beim letzten Commit erfolgreich.",
		msgPreviewTeardown: " Die Vorschau wird in Kürze abgebaut.",
		msgCIFailed:        "CI ist beim letzten Push fehlgeschlagen.",
		msgCIFailedJob:     "CI ist beim letzten Push fehlgeschlagen. Der Job `%s` ist fehlgeschlagen:",
		msgViewRun:         "Run ansehen",
		msgOpenPreview:     "Vorschau öffnen",
		msgLogs:            "Logs",
		msgNote:            "Hinweis:",
		msgUpdate:          "Update: %s",

		MsgErrUnavailable: "Der KI-Service ist vorübergehend nicht erreichbar. Bitte versuche es in ein paar Minuten erneut.",
		MsgErrOverloaded:  "Der KI-Service ist gerade überlastet. Bitte versuche es in ein paar Minuten erneut.",
		MsgErrRateLimit:   "Der KI-Service ist gerade rate-limited. Bitte versuche es gleich nochmal.",
		MsgErrGeneric:     "Entschuldigung, bei der Verarbeitung deiner Anfrage ist ein Fehler aufgetreten. Bitte versuche es erneut.",

		MsgFallbackCreated:   "Erstellt: <%s|#%d>: %s",
		MsgFallbackDelegated: "Delegiert an %s",
	},
}

// LocalizedMsg returns the translation for key in the given language, falling back to English.
func LocalizedMsg(lang string, key MessageKey) string {
	if msgs, ok := translations[lang]; ok {
		if s, ok := msgs[key]; ok {
			return s
		}
	}
	return translations["en"][key]
}
