package pbar

import (
	"fmt"
	"math"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

type ProgressBar struct {
	actual     uint16 // Actual number of iterations
	Total      int    // Total number of iterations to sum 100%
	header     uint16 // Header length, to be used to calculate the bar width "Progress: [100%] []"
	wscol      uint16 // Window width
	wsrow      uint16 // Window height
	doneStr    string // Progress bar done string
	ongoingStr string // Progress bar ongoing string
	progressCh chan int
	async      bool
	multiLine  bool
	sigTermCh  chan os.Signal
	once       sync.Once // Close the signal channel only once
	winSize    struct {  // winSize is the struct to store the current window size, used by ioctl
		Row    uint16 // row
		Col    uint16 // column
		Xpixel uint16 // X pixel
		Ypixel uint16 // Y pixel
	}
}

func NewProgressBar(total int) *ProgressBar {
	pb := &ProgressBar{
		progressCh: make(chan int),
		Total:      total,
		header:     0,
		wscol:      0,
		wsrow:      0,
		doneStr:    "#",
		ongoingStr: " ",
		sigTermCh:  make(chan os.Signal, 1),
		async:      false,
	}

	pb.updateWSize()
	pb.signalHandler()

	return pb
}

func NewAsyncProgressBar(total int) *ProgressBar {
	pb := NewProgressBar(total)
	pb.async = true

	go func() {
		for {
			select {
			case p := <-pb.progressCh:
				pb.Add(p)
			}
		}
	}()

	return pb
}

func NewMultilineProgressBar(total int) *ProgressBar {
	pb := NewProgressBar(total)
	pb.multiLine = true

	// go func() {
	// 	for {
	// 		select {
	// 		case p := <-pb.progressCh:
	// 			pb.Add(p)
	// 		}
	// 	}
	// }()

	return pb
}

func (pb *ProgressBar) AddAsync(count int) {
	if pb.async == true {
		if count+int(pb.actual) >= pb.Total {
			pb.cleanUp()
		} else {
			pb.progressCh <- count
		}
	}
}

// Add and render the progress bar. Receives the count to add to the actual progression
func (pb *ProgressBar) Add(count int) {
	if pb.winSize.Col == 0 || pb.winSize.Row == 0 {
		// Not a terminal, running in a pipeline or test
		return
	}

	if pb.actual+uint16(count) <= uint16(pb.Total) {
		pb.actual += uint16(count)
	}

	if pb.multiLine == true {
		fmt.Print("\x1B7")       // Save the cursor position
		fmt.Print("\x1B[2K")     // Erase the entire line
		fmt.Print("\x1B[0J")     // Erase from cursor to end of screen
		fmt.Print("\x1B[?47h")   // Save screen
		fmt.Print("\x1B[1J")     // Erase from cursor to beginning of screen
		fmt.Print("\x1B[?47l")   // Restore screen
		defer fmt.Print("\x1B8") // Restore the cursor position util new size is calculated

		// move cursor to row #, col #
		fmt.Printf("\x1B[%d;%dH", pb.wsrow, 0)
	} else {
		fmt.Print("\u001b[1000D")
	}

	barWidth := int(math.Abs(float64(pb.wscol - pb.header)))                   // Calculate the bar width
	barDone := int(float64(barWidth) * float64(pb.actual) / float64(pb.Total)) // Calculate the bar done length
	done := strings.Repeat(pb.doneStr, barDone)                                // Fill the bar with done string
	todo := strings.Repeat(pb.ongoingStr, barWidth-barDone)                    // Fill the bar with todo string
	bar := fmt.Sprintf("[%s%s]", done, todo)                                   // Combine the done and todo string

	percent := int(float64(pb.actual) / float64(pb.Total) * 100.0)

	switch {
	case pb.wscol >= uint16(0) && pb.wscol <= uint16(9):
		fmt.Printf("[\x1B[33m%3d%%\x1B[0m]", percent)
	case pb.wscol >= uint16(10) && pb.wscol <= uint16(25):
		fmt.Printf("[\x1B[33m%3d%%\x1B[0m] %s", percent, bar)
	default:
		if pb.actual >= uint16(pb.Total) {
			pb.cleanUp()
			fmt.Printf("\x1b[1;92mFinished: \x1b[1;0m[\x1B[1;33m%3d%%\x1B[0m]\n", percent)
		} else {
			fmt.Printf("\x1b[1;94mProgress: \x1b[1;0m[\x1B[1;33m%3d%%\x1B[0m] %s", percent, bar)
		}
	}
}

// cleanUp restore reserved bottom line and restore cursor position
func (pb *ProgressBar) cleanUp() {
	// Close the signal channel politely, avoid closing it twice
	pb.once.Do(func() { close(pb.sigTermCh) })

	if pb.winSize.Col == 0 || pb.winSize.Row == 0 {
		// Not a terminal, running in a pipeline or test
		return
	}

	if pb.multiLine == true {
		fmt.Print("\x1B7")                 // Save the cursor position
		fmt.Printf("\x1B[0;%dr", pb.wsrow) // Drop margin reservation
		fmt.Printf("\x1B[%d;0f", pb.wsrow) // Move the cursor to the bottom line
		fmt.Print("\x1B[0K")               // Erase the entire line
		fmt.Print("\x1B8")                 // Restore the cursor position util new size is calculated
	}
}

// updateWSize update the window size
func (pb *ProgressBar) updateWSize() error {
	isTerminal, err := pb.checkIsTerminal()
	if err != nil {
		return fmt.Errorf("could not check if the current process is running in a terminal: %w", err)
	}
	if !isTerminal {
		return nil // Not a terminal, running in a pipeline or test
	}

	pb.wscol = pb.winSize.Col
	pb.wsrow = pb.winSize.Row

	switch {
	case pb.wscol >= uint16(0) && pb.wscol <= uint16(9):
		pb.header = uint16(6) // len("[100%]") is the minimum header length
	case pb.wscol >= uint16(10) && pb.wscol <= uint16(20):
		pb.header = uint16(9) // len("[100%] []") is the midium header length
	default:
		pb.header = uint16(19) // len("Progress: [100%] []") is the maximum header length
	}

	fmt.Print("\x1BD")                   // Return carriage
	fmt.Print("\x1B7")                   // Save the cursor position
	fmt.Printf("\x1B[0;%dr", pb.wsrow-1) // Reserve the bottom line
	fmt.Print("\x1B8")                   // Restore the cursor position
	fmt.Print("\x1B[1A")                 // Moves cursor up # lines

	return nil
}

func (pb *ProgressBar) signalHandler() {
	// Register term signals
	signal.Notify(pb.sigTermCh, syscall.SIGWINCH)
	signal.Notify(pb.sigTermCh, syscall.SIGTERM)
	signal.Notify(pb.sigTermCh, syscall.SIGINT)
	signal.Notify(pb.sigTermCh, syscall.SIGKILL)

	go func() {
		for {
			select {
			case signal := <-pb.sigTermCh:
				switch signal {
				case syscall.SIGWINCH:
					if err := pb.updateWSize(); err != nil {
						panic(err) // The window size could not be updated
					}

				case syscall.SIGTERM, syscall.SIGINT, syscall.SIGKILL:
					// Restore reserved bottom line
					pb.cleanUp()
					// Exit gracefully but exit code 1
					os.Exit(1)
				}
			}
		}
	}()
}

// checkIsTerminal check if the current process is running in a terminal
func (pb *ProgressBar) checkIsTerminal() (isTerminal bool, err error) {
	if _, _, err := syscall.Syscall(
		syscall.SYS_IOCTL,
		uintptr(syscall.Stdin),
		uintptr(syscall.TIOCGWINSZ),
		uintptr(unsafe.Pointer(&pb.winSize))); err != 0 {
		if err == syscall.ENOTTY || err == syscall.ENODEV {
			// Not a terminal, running in a pipeline or test
			return false, nil
		} else {
			// Other error
			return false, err
		}
	}

	return true, nil
}
