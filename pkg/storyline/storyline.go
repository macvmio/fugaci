package storyline

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type Logger interface {
	Println(args ...any)
}

type StoryLine struct {
	parts  []string
	mu     sync.Mutex // To ensure thread-safety if used in concurrent environments
	logger Logger     // Custom logger
}

// New initializes and returns a new StoryLine instance with a custom logger
func New(logger Logger) *StoryLine {

	return &StoryLine{
		parts:  make([]string, 0, 10), // Initial capacity to avoid frequent allocations
		logger: logger,
	}
}

// Add adds a key=value pair to the log line
// If the value contains whitespace, it adds quotes around it.
func (l *StoryLine) Add(key string, value interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Convert value to string using fmt.Sprintf (it will call the String() method if available)
	strValue := fmt.Sprintf("%v", value)

	// Add quotes around the value if it contains any whitespace
	if strings.ContainsAny(strValue, " \t\n") || strValue == "" {
		strValue = fmt.Sprintf(`"%s"`, strValue) // Use double quotes for the value
	}

	// Append the key=value pair to parts
	l.parts = append(l.parts, key+"="+strValue)
}

// Log prints the log line as a single entry
func (l *StoryLine) Log() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.logger.Println(strings.Join(l.parts, ", "))
}

// Clear clears the log line to reuse the instance
func (l *StoryLine) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.parts = l.parts[:0] // Efficiently resets the slice without reallocating memory
}

// AddTimeMs adds a key=value pair with a duration in milliseconds
func (l *StoryLine) AddTimeMs(key string, duration time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.parts = append(l.parts, fmt.Sprintf("%s=%dms", key, duration.Milliseconds()))
}

// AddElapsedTimeSince adds the time elapsed since a given start time in milliseconds
func (l *StoryLine) AddElapsedTimeSince(key string, startTime time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()

	elapsed := time.Since(startTime)
	l.parts = append(l.parts, fmt.Sprintf("%s_ms=%d", key, elapsed.Milliseconds()))
}
