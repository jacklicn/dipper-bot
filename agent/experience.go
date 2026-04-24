package agent

import "github.com/jacklicn/dipper-bot/config"

func effectiveMemoryNudgeEvery(exp config.AgentExperienceConfig) int {
	if exp.MemoryPromptNudgeEvery == nil {
		return 10
	}
	return *exp.MemoryPromptNudgeEvery
}

func effectiveSkillNudgeEvery(exp config.AgentExperienceConfig) int {
	if exp.SkillPromptNudgeEvery == nil {
		return 10 // align with memory nudge default when unset (LoadConfig also merges to &10)
	}
	v := *exp.SkillPromptNudgeEvery
	if v <= 0 {
		return 0 // explicit &0 disables
	}
	return v
}

func effectiveLearnerFeedbackInstantPush(exp config.AgentExperienceConfig) bool {
	if exp.LearnerFeedbackInstantPush == nil {
		return true
	}
	return *exp.LearnerFeedbackInstantPush
}
