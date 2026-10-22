package common

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
)

const mediaUploadWorkers = 10

// UploadMediaGroup keeps the original order and type-local numbering of references.
type UploadMediaGroup struct {
	Label string
	Paths []string
}

// UploadMediaGroups waits for every upload, preserving group and path order even
// when requests finish out of order. Any failure prevents generation callers
// from receiving a partial set of asset IDs.
func UploadMediaGroups(ctx context.Context, runner *Runner, groups ...UploadMediaGroup) ([][]string, error) {
	type task struct {
		group int
		index int
		slot  int
	}
	assetIDs := make([][]string, len(groups))
	var tasks []task
	for g, group := range groups {
		assetIDs[g] = make([]string, len(group.Paths))
		for i := range group.Paths {
			tasks = append(tasks, task{group: g, index: i, slot: len(tasks)})
		}
	}
	errs := make([]error, len(tasks))
	taskCh := make(chan task)
	var wg sync.WaitGroup
	for worker := 0; worker < min(mediaUploadWorkers, len(tasks)); worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskCh {
				group := groups[task.group]
				path := group.Paths[task.index]
				expanded, err := ExpandPath(path)
				if err != nil {
					errs[task.slot] = fmt.Errorf("第%d%s %q 路径解析失败: %w", task.index+1, group.Label, filepath.Base(path), err)
					continue
				}
				result, err := UploadFile(ctx, UploadFileOptions{Path: expanded}, runner)
				if err != nil {
					errs[task.slot] = fmt.Errorf("第%d%s %q 上传失败: %w", task.index+1, group.Label, filepath.Base(path), err)
					continue
				}
				assetIDs[task.group][task.index] = result.AssetID
			}
		}()
	}
	for _, task := range tasks {
		taskCh <- task
	}
	close(taskCh)
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("参考素材上传失败，未提交生成：\n%w", err)
	}
	return assetIDs, nil
}
