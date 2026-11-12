package css

// RuleMutationError is a DOMException a rule mutation throws, named by its
// DOMExceptionCode (IndexSizeError, SyntaxError, HierarchyRequestError,
// InvalidStateError).
type RuleMutationError struct{ Name, Message string }

func (e *RuleMutationError) Error() string { return e.Name + ": " + e.Message }

// RuleCount, InsertRule and DeleteRule are not yet ported (LOU-866).
func (s *StyleSheetContents) RuleCount() int { return 0 }

func (s *StyleSheetContents) InsertRule(rule string, index int) (int, error) {
	return 0, &RuleMutationError{Name: "NotSupportedError"}
}

func (s *StyleSheetContents) DeleteRule(index int) error {
	return &RuleMutationError{Name: "NotSupportedError"}
}
