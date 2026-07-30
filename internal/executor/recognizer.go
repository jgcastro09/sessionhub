package executor

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jgcastro09/sessionhub/internal/domain"
)

type Recognition struct {
	Matched   bool
	Ambiguous bool
	RuleID    string
	Outcome   domain.State
	Reason    string
}

func RecognizeOutput(rules []domain.RecognitionRule, output string) Recognition {
	var matches []domain.RecognitionRule
	for _, rule := range rules {
		switch rule.Kind {
		case domain.RecognizeLiteral, domain.RecognizePrompt:
			if rule.Value != "" && strings.Contains(output, rule.Value) {
				matches = append(matches, rule)
			}
		case domain.RecognizePattern:
			pattern, err := regexp.Compile(rule.Value)
			if err == nil && pattern.MatchString(output) {
				matches = append(matches, rule)
			}
		}
	}
	return selectRecognition(matches, "terminal output")
}

func RecognizeExit(rules []domain.RecognitionRule, exitCode int) Recognition {
	var matches []domain.RecognitionRule
	for _, rule := range rules {
		switch rule.Kind {
		case domain.RecognizeProcessExit:
			matches = append(matches, rule)
		case domain.RecognizeExitCode:
			code, err := strconv.Atoi(strings.TrimSpace(rule.Value))
			if err == nil && code == exitCode {
				matches = append(matches, rule)
			}
		}
	}
	return selectRecognition(matches, "process exit")
}

func RecognizeStable(rules []domain.RecognitionRule, quietFor time.Duration) Recognition {
	var matches []domain.RecognitionRule
	for _, rule := range rules {
		if rule.Kind != domain.RecognizeStable {
			continue
		}
		duration, err := time.ParseDuration(rule.Value)
		if err == nil && duration > 0 && quietFor >= duration {
			matches = append(matches, rule)
		}
	}
	return selectRecognition(matches, "stable terminal output")
}

func selectRecognition(matches []domain.RecognitionRule, reason string) Recognition {
	if len(matches) == 0 {
		return Recognition{}
	}
	bestPriority := matches[0].Priority
	for _, match := range matches[1:] {
		if match.Priority > bestPriority {
			bestPriority = match.Priority
		}
	}
	var best []domain.RecognitionRule
	for _, match := range matches {
		if match.Priority == bestPriority {
			best = append(best, match)
		}
	}
	if len(best) > 1 {
		outcome := best[0].Outcome
		for _, match := range best[1:] {
			if match.Outcome != outcome {
				return Recognition{
					Matched: true, Ambiguous: true, Outcome: domain.StateWaiting,
					Reason: "ambiguous recognition rules require manual confirmation",
				}
			}
		}
	}
	return Recognition{
		Matched: true, RuleID: best[0].ID, Outcome: best[0].Outcome,
		Reason: reason + " matched rule " + best[0].Name,
	}
}
