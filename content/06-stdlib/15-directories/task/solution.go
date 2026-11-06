package task

import (
	"io/fs"
	"slices"
)

// FindLarge возвращает пути файлов размером не меньше minSize,
// пропуская каталоги с именами из skipDirs.
func FindLarge(fsys fs.FS, minSize int64, skipDirs []string) ([]string, error) {
	var found []string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// SkipDir только для каталогов: для файла он пропустил бы
			// всех оставшихся соседей.
			if p != "." && slices.Contains(skipDirs, d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() >= minSize {
			found = append(found, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// WalkDir сортирует имена внутри каталога, но полные пути в порядке
	// обхода могут быть не отсортированы: "a/x" идёт раньше "a-b/y".
	slices.Sort(found)
	return found, nil
}
