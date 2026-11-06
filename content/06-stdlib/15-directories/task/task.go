package task

import "io/fs"

// FindLarge возвращает пути файлов размером не меньше minSize,
// пропуская каталоги с именами из skipDirs.
func FindLarge(fsys fs.FS, minSize int64, skipDirs []string) ([]string, error) {
	// TODO: реализуйте функцию
	return nil, nil
}
