// Package reporter summarizes run stats.
package reporter

import "fmt"

type Reporter struct {
	Chunks       int
	Blocks       int
	Valid        int
	Skipped      int
	FilesWritten int
	Errors       []string
}

func (r *Reporter) AddChunk(n int)      { r.Chunks += n }
func (r *Reporter) AddBlocks(n int)     { r.Blocks += n }
func (r *Reporter) AddValid(n int)      { r.Valid += n }
func (r *Reporter) AddSkipped(n int)    { r.Skipped += n }
func (r *Reporter) AddFiles(n int)      { r.FilesWritten += n }
func (r *Reporter) AddError(msg string) { r.Errors = append(r.Errors, msg) }

func (r *Reporter) Summary() string {
	s := fmt.Sprintf("chunks: %d\nblocks: %d\nvalid: %d\nskipped: %d\nfiles_written: %d", r.Chunks, r.Blocks, r.Valid, r.Skipped, r.FilesWritten)
	if len(r.Errors) > 0 {
		s += "\nerrors:"
		for _, e := range r.Errors {
			s += "\n  - " + e
		}
	}
	return s
}

func (r *Reporter) Print() {
	fmt.Println(r.Summary())
}
