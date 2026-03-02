package output

import (
	"fmt"
	"sync"
	"time"
)

// Spinner shows an animated spinner on a single line for human mode.
// In agent mode it's a no-op.
type Spinner struct {
	msg    string
	done   chan struct{}
	once   sync.Once
	frames []string
}

// NewSpinner creates and starts a spinner with the given message.
func NewSpinner(msg string) *Spinner {
	s := &Spinner{
		msg:    msg,
		done:   make(chan struct{}),
		frames: []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
	}
	if !IsHuman {
		// Agent mode — just print once
		fmt.Printf("%s\n", msg)
		return s
	}
	go s.run()
	return s
}

func (s *Spinner) run() {
	i := 0
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			fmt.Printf("\r  %s %s  ", Cyan(s.frames[i%len(s.frames)]), s.msg)
			i++
		}
	}
}

// Stop stops the spinner and clears the line.
func (s *Spinner) Stop() {
	s.once.Do(func() {
		close(s.done)
		if IsHuman {
			fmt.Print("\r\033[K") // clear line
		}
	})
}

// StopWith stops the spinner and replaces it with a final message.
func (s *Spinner) StopWith(icon, msg string) {
	s.once.Do(func() {
		close(s.done)
		if IsHuman {
			fmt.Printf("\r\033[K  %s %s\n", icon, msg)
		} else {
			fmt.Printf("%s\n", msg)
		}
	})
}

// StopSuccess stops with a green checkmark.
func (s *Spinner) StopSuccess(msg string) {
	s.StopWith(Green("✓"), msg)
}

// StopWarning stops with a yellow warning sign.
func (s *Spinner) StopWarning(msg string) {
	s.StopWith(Yellow("⚠"), msg)
}

// StopError stops with a red X.
func (s *Spinner) StopError(msg string) {
	s.StopWith(Red("✗"), msg)
}
