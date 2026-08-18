package bulkcsv

import (
	"bufio"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type countingReader struct {
	reader io.Reader
	read   int64
}

func (r *countingReader) Read(buffer []byte) (int, error) {
	n, err := r.reader.Read(buffer)
	r.read += int64(n)
	return n, err
}

type csvScanner struct {
	name       string
	file       *os.File
	reader     *csv.Reader
	counter    *countingReader
	size       int64
	rows       int64
	started    time.Time
	lastReport time.Time
	logger     *slog.Logger
}

func openCSV(directory, name string, expectedHeader []string, logger *slog.Logger) (*csvScanner, error) {
	path := filepath.Join(directory, name)
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", name, err)
	}

	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("stat %s: %w", name, err)
	}

	counter := &countingReader{reader: file}
	reader := csv.NewReader(bufio.NewReaderSize(counter, 256*1024))
	reader.ReuseRecord = true
	header, err := reader.Read()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("read %s header: %w", name, err)
	}
	if len(header) > 0 {
		header[0] = strings.TrimPrefix(header[0], "\ufeff")
	}
	if !slices.Equal(header, expectedHeader) {
		file.Close()
		return nil, fmt.Errorf("validate %s header: got %q, want %q", name, header, expectedHeader)
	}
	reader.FieldsPerRecord = len(expectedHeader)

	now := time.Now()
	return &csvScanner{
		name:       name,
		file:       file,
		reader:     reader,
		counter:    counter,
		size:       info.Size(),
		started:    now,
		lastReport: now,
		logger:     logger,
	}, nil
}

func (s *csvScanner) Scan(ctx context.Context, visit func([]string) error) error {
	defer s.file.Close()

	for {
		if s.rows%100_000 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
			s.report(false)
		}

		record, err := s.reader.Read()
		if err == io.EOF {
			s.report(true)
			return nil
		}
		if err != nil {
			return fmt.Errorf("read %s record %d: %w", s.name, s.rows+2, err)
		}
		s.rows++
		if err := visit(record); err != nil {
			return fmt.Errorf("process %s record %d: %w", s.name, s.rows+1, err)
		}
	}
}

func (s *csvScanner) report(final bool) {
	if s.logger == nil {
		return
	}
	now := time.Now()
	if !final && now.Sub(s.lastReport) < 5*time.Second {
		return
	}
	elapsed := now.Sub(s.started)
	bytesPerSecond := float64(0)
	if elapsed > 0 {
		bytesPerSecond = float64(s.counter.read) / elapsed.Seconds()
	}
	s.logger.Info("USDA CSV progress",
		"file", s.name,
		"rows", s.rows,
		"bytes", s.counter.read,
		"size_bytes", s.size,
		"megabytes_per_second", bytesPerSecond/(1024*1024),
		"elapsed", elapsed.Round(time.Millisecond),
		"complete", final,
	)
	s.lastReport = now
}
