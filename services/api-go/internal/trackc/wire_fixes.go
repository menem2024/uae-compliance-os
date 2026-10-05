package trackc

// wireFixes is where Task 20 (gate GB-2) sets Module.Fixes and Validation.OnRun. Until then the
// fixes route answers fix_unavailable and validation runs trigger nothing.
func wireFixes(*Module) {}
