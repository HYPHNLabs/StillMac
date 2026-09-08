package residue

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type measurement struct {
	Size         Size
	State        string
	Identity     string
	Fingerprint  string
	Evidence     []string
	Files        map[fileKey]int64
	ItemsVisited int
	ItemsSkipped int
}

func measureTree(root string, budget *scanBudget) measurement {
	result := measurement{Files: make(map[fileKey]int64)}
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		result.Size = Size{Status: SizeNotPresent}
		result.State = "not-present"
		result.Evidence = []string{"root_not_present"}
		return result
	}
	if err != nil {
		result.Size = Size{Status: SizeUnknown}
		result.State = "unknown"
		result.Evidence = []string{"root_unavailable"}
		return result
	}
	if info.Mode()&os.ModeSymlink != 0 {
		result.Size = Size{Status: SizeUnknown}
		result.State = "unsafe-root"
		result.Evidence = []string{"root_symlink", "no_traversal"}
		return result
	}
	if !info.IsDir() {
		result.Size = Size{Status: SizeUnknown}
		result.State = "unsafe-root"
		result.Evidence = []string{"root_not_directory", "no_traversal"}
		return result
	}
	result.Identity = identityHash(info)
	if result.Identity == "" {
		result.Size = Size{Status: SizeUnknown}
		result.State = "unknown"
		result.Evidence = []string{"root_identity_unavailable", "no_traversal"}
		return result
	}

	budget.visited = make(map[fileKey]struct{})
	hash := sha256.New()
	partial := false
	unknown := false
	measured := false
	stopped := false
	var visit func(path, relative string, depth int) error
	visit = func(path, relative string, depth int) error {
		if stopped {
			return nil
		}
		if time.Now().After(budget.deadline) {
			partial = true
			result.ItemsSkipped++
			result.Evidence = append(result.Evidence, "time_bound_reached")
			stopped = true
			return nil
		}
		if budget.items >= budget.limits.MaxItems {
			partial = true
			result.ItemsSkipped++
			result.Evidence = append(result.Evidence, "item_bound_reached")
			stopped = true
			return nil
		}
		budget.items++
		result.ItemsVisited++
		entryInfo, statErr := os.Lstat(path)
		if statErr != nil {
			if errors.Is(statErr, os.ErrPermission) {
				result.Evidence = append(result.Evidence, "permission_denied")
			} else {
				result.Evidence = append(result.Evidence, "metadata_unavailable")
			}
			result.ItemsSkipped++
			if relative == "." {
				unknown = true
			} else {
				partial = true
			}
			return nil
		}
		if entryInfo.Mode()&os.ModeSymlink != 0 {
			partial = true
			result.ItemsSkipped++
			result.Evidence = append(result.Evidence, "symlink_skipped")
			return nil
		}
		if entryInfo.IsDir() {
			writeFingerprint(hash, relative, entryInfo)
			children, readErr := os.ReadDir(path)
			if readErr != nil {
				result.ItemsSkipped++
				if errors.Is(readErr, os.ErrPermission) {
					result.Evidence = append(result.Evidence, "permission_denied")
				} else {
					result.Evidence = append(result.Evidence, "directory_unreadable")
				}
				if relative == "." && !measured {
					unknown = true
				} else {
					partial = true
				}
				return nil
			}
			if depth >= budget.limits.MaxDepth && len(children) > 0 {
				partial = true
				result.ItemsSkipped += len(children)
				result.Evidence = append(result.Evidence, "depth_bound_reached")
				return nil
			}
			for _, child := range children {
				childRelative := child.Name()
				if relative != "." {
					childRelative = filepath.Join(relative, child.Name())
				}
				if err := visit(filepath.Join(path, child.Name()), childRelative, depth+1); err != nil {
					return err
				}
				if stopped {
					break
				}
			}
			return nil
		}
		if !entryInfo.Mode().IsRegular() {
			partial = true
			result.ItemsSkipped++
			result.Evidence = append(result.Evidence, "special_file_skipped")
			return nil
		}
		if entryInfo.Size() < 0 {
			partial = true
			result.ItemsSkipped++
			result.Evidence = append(result.Evidence, "negative_size")
			return nil
		}
		writeFingerprint(hash, relative, entryInfo)
		key, keyOK := statKey(entryInfo)
		if !keyOK {
			unknown = true
			result.ItemsSkipped++
			result.Evidence = append(result.Evidence, "file_identity_unavailable")
			return nil
		}
		if _, duplicate := budget.visited[key]; duplicate {
			budget.duplicates++
			result.ItemsSkipped++
			result.Evidence = append(result.Evidence, "hardlink_deduplicated")
			return nil
		}
		budget.visited[key] = struct{}{}
		if entryInfo.Size() > budget.limits.MaxBytes-budget.bytes {
			partial = true
			result.ItemsSkipped++
			result.Evidence = append(result.Evidence, "byte_bound_reached")
			stopped = true
			return nil
		}
		budget.bytes += entryInfo.Size()
		result.Files[key] = entryInfo.Size()
		measured = true
		return nil
	}

	if err := visit(root, ".", 0); err != nil && !errors.Is(err, io.EOF) {
		unknown = true
		result.Evidence = append(result.Evidence, "measurement_failed")
	}
	result.Evidence = uniqueStrings(result.Evidence)
	bytes := int64(0)
	for _, size := range result.Files {
		if next, ok := safeAdd(bytes, size); ok {
			bytes = next
		} else {
			unknown = true
		}
	}
	switch {
	case unknown && !measured:
		result.Size = Size{Status: SizeUnknown}
		result.State = "unknown"
	case unknown || partial:
		result.Size = Size{Status: SizePartial, Bytes: &bytes}
		result.State = "partial"
	case stopped:
		result.Size = Size{Status: SizePartial, Bytes: &bytes}
		result.State = "partial"
	default:
		result.Size = Size{Status: SizeComplete, Bytes: &bytes}
		result.State = "measured"
		result.Fingerprint = hexDigest(hash.Sum(nil))
	}
	if result.Size.Status != SizeComplete {
		result.Fingerprint = ""
	}
	return result
}

func writeFingerprint(writer io.Writer, relative string, info os.FileInfo) {
	_, _ = fmt.Fprintf(writer, "%s\x00%d\x00%d\x00", relative, info.Mode().Type(), info.Size())
}

func statKey(info os.FileInfo) (fileKey, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fileKey{}, false
	}
	return fileKey{device: uint64(stat.Dev), inode: uint64(stat.Ino)}, true
}

func hexDigest(value []byte) string {
	const hexChars = "0123456789abcdef"
	result := make([]byte, len(value)*2)
	for index, byteValue := range value {
		result[index*2] = hexChars[byteValue>>4]
		result[index*2+1] = hexChars[byteValue&0x0f]
	}
	return string(result)
}
