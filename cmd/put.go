// Copyright © 2016 Dropbox, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/dropbox/dropbox-sdk-go-unofficial/dropbox/files"
	"github.com/dustin/go-humanize"
	"github.com/mitchellh/ioprogress"
	"github.com/spf13/cobra"
)

const chunkSize int64 = 1 << 24

func uploadChunked(dbx files.Client, r io.Reader, commitInfo *files.CommitInfo, sizeTotal int64) (err error) {
	res, err := dbx.UploadSessionStart(files.NewUploadSessionStartArg(),
		&io.LimitedReader{R: r, N: chunkSize})
	if err != nil {
		return
	}

	written := chunkSize

	for (sizeTotal - written) > chunkSize {
		args := files.NewUploadSessionCursor(res.SessionId, uint64(written))

		err = dbx.UploadSessionAppend(args, &io.LimitedReader{R: r, N: chunkSize})
		if err != nil {
			return
		}
		written += chunkSize
	}

	cursor := files.NewUploadSessionCursor(res.SessionId, uint64(written))
	args := files.NewUploadSessionFinishArg(cursor, commitInfo)

	if _, err = dbx.UploadSessionFinish(args, r); err != nil {
		return
	}

	return
}

func put(cmd *cobra.Command, args []string) (err error) {
	if len(args) == 0 {
		return errors.New("missing operands to `put`")
	}

	destination, err := cmd.Flags().GetString("destination")
	if err != nil {
		return err
	}

	var waitGroup sync.WaitGroup
	for _, arg := range args {
		waitGroup.Add(1)
		go func(arg string) error {
			defer waitGroup.Done()
			dst := "/" + path.Base(arg)

			if destination != "" {
				dst, err = validatePath(fullName(arg, destination))
				if err != nil {
					return err
				}
			}

			contents, err := os.Open(arg)
			defer contents.Close()
			if err != nil {
				return err
			}

			contentsInfo, err := contents.Stat()
			if err != nil {
				return err
			}

			progressbar := &ioprogress.Reader{
				Reader: contents,
				DrawFunc: ioprogress.DrawTerminalf(os.Stderr, func(progress, total int64) string {
					return fmt.Sprintf("Uploading %s/%s",
						humanize.IBytes(uint64(progress)), humanize.IBytes(uint64(total)))
				}),
				Size: contentsInfo.Size(),
			}

			commitInfo := files.NewCommitInfo(dst)
			commitInfo.Mode.Tag = "overwrite"

			// The Dropbox API only accepts timestamps in UTC with second precision.
			commitInfo.ClientModified = time.Now().UTC().Round(time.Second)

			dbx := files.New(config)
			if contentsInfo.Size() > chunkSize {
				err = uploadChunked(dbx, progressbar, commitInfo, contentsInfo.Size())
				if err != nil {
					return err
				}
			}

			if uploadFile(dbx, commitInfo, progressbar) != nil {
				return fmt.Errorf("Did not upload %s", arg)
			}

			return nil
		}(arg)
	}
	waitGroup.Wait()
	return nil
}

func uploadFile(dbx files.Client, commitInfo *files.CommitInfo, progressbar *ioprogress.Reader) error {
	if _, err := dbx.Upload(commitInfo, progressbar); err != nil {
		return err
	}
	return nil
}

func fullName(fileName, destination string) string {
	return strings.TrimSuffix(destination, "/") + "/" + fileName
}

// putCmd represents the put command
var putCmd = &cobra.Command{
	Use:   "put [flags] <source>",
	Short: "Upload files",
	RunE:  put,
}

func init() {
	RootCmd.AddCommand(putCmd)
	putCmd.Flags().StringP("destination", "d", "", "specify a destination")
	putCmd.Flags().BoolP("force", "f", false, "specify to overwrite existing files")

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// putCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// putCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")

}
