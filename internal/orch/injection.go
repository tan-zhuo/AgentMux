package orch

import (
	"regexp"
	"strings"
)

// Everything a tool reads is untrusted. `tmux capture-pane` returns whatever an
// AI agent on a remote machine printed, which includes the repository files it
// read, the CI logs it fetched, the README of a dependency and the text of an
// issue. Any of those can contain a sentence addressed to whoever reads it next
// — and the thing reading it next holds SSH tools.
//
// Blocking on these patterns is not an option: "ignore the previous output" is
// an ordinary sentence in a log, and a system that refuses to work whenever a
// log is chatty gets turned off. So the input is wrapped as data, the tools are
// gated, and this only raises a flag — visible in the log and on the approval
// card, where a person can weigh it.
var injectionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)ignore (all |any )?(the )?(previous|prior|above|earlier) (instructions?|prompts?|rules?|messages?)`),
	regexp.MustCompile(`(?i)disregard (the )?(above|previous|earlier|prior)`),
	regexp.MustCompile(`(?i)forget (everything|all) (you|that)`),
	regexp.MustCompile(`(?i)you are now (a|an|the)\b`),
	regexp.MustCompile(`(?i)new (system )?(instructions?|prompt|rules?)\s*[:：]`),
	regexp.MustCompile(`(?i)(do not|don't) (tell|inform|ask) (the )?(user|human|operator)`),
	regexp.MustCompile(`(?i)without (asking|confirming|approval)`),
	regexp.MustCompile(`(?i)</?(system|assistant)>`),
	// Chinese phrasings of the same moves.
	regexp.MustCompile(`忽略(之前|以上|前面)(的)?(所有)?(指令|指示|提示|规则)`),
	regexp.MustCompile(`你现在是`),
	regexp.MustCompile(`不要(告诉|通知|询问)(用户|使用者|人类)`),
	regexp.MustCompile(`(无需|不用|不需要)(确认|审批|经过同意)`),
}

// SuspectsInjection reports whether text contains something shaped like an
// instruction aimed at the model, and what it found.
//
// The phrase is returned rather than a bare boolean because "this looks like an
// injection" is not actionable, while "this log contains the words «ignore the
// previous instructions»" is something a person can judge in a second.
func SuspectsInjection(text string) (bool, string) {
	for _, re := range injectionPatterns {
		if m := re.FindString(text); m != "" {
			return true, strings.TrimSpace(m)
		}
	}
	return false, ""
}
