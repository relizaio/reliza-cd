/*
The MIT License (MIT)

Copyright (c) 2022-2026 Reliza Incorporated (Reliza (tm), https://reliza.io)

Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"),
to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense,
and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
*/
package cli

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/robfig/cron/v3"
)

type BackupConfig struct {
	Enabled            bool
	Schedule           string
	Prefix             string
	AwsRegion          string
	AwsBucket          string
	AwsAccessKeyId     string
	AwsSecretAccessKey string
	EncryptionPassword string
}

var backupConfig BackupConfig

func initBackupConfig() {
	backupConfig.Enabled = strings.ToLower(os.Getenv("BACKUP_ENABLED")) == "true"
	backupConfig.Schedule = os.Getenv("BACKUP_SCHEDULE")
	backupConfig.Prefix = os.Getenv("BACKUP_PREFIX")
	backupConfig.AwsRegion = os.Getenv("AWS_REGION")
	backupConfig.AwsBucket = os.Getenv("AWS_BUCKET")
	backupConfig.AwsAccessKeyId = os.Getenv("AWS_ACCESS_KEY_ID")
	backupConfig.AwsSecretAccessKey = os.Getenv("AWS_SECRET_ACCESS_KEY")
	backupConfig.EncryptionPassword = os.Getenv("ENCRYPTION_PASSWORD")
}

func StartBackupScheduler() {
	initBackupConfig()
	if !backupConfig.Enabled {
		sugar.Info("Backup is disabled")
		return
	}

	if len(backupConfig.Schedule) == 0 {
		sugar.Error("BACKUP_ENABLED is true but BACKUP_SCHEDULE is not set")
		return
	}
	if len(backupConfig.AwsBucket) == 0 {
		sugar.Error("BACKUP_ENABLED is true but AWS_BUCKET is not set")
		return
	}
	if len(backupConfig.AwsRegion) == 0 {
		sugar.Error("BACKUP_ENABLED is true but AWS_REGION is not set")
		return
	}
	if len(backupConfig.EncryptionPassword) == 0 {
		sugar.Error("BACKUP_ENABLED is true but ENCRYPTION_PASSWORD is not set")
		return
	}

	c := cron.New()
	_, err := c.AddFunc(backupConfig.Schedule, runBackup)
	if err != nil {
		sugar.Error("Failed to parse BACKUP_SCHEDULE cron expression: ", err)
		return
	}
	c.Start()
	sugar.Info("Backup scheduler started with schedule: ", backupConfig.Schedule)
}

func runBackup() {
	sugar.Info("Starting workspace backup")
	timestamp := time.Now().UTC().Format("20060102-150405")
	tarFile := "/tmp/workspace-backup-" + timestamp + ".tar.gz"
	encFile := tarFile + ".enc"

	_, stderr, err := shellout("tar -czf " + tarFile + " -C /app workspace")
	if err != nil {
		sugar.Error("Failed to create tar.gz of workspace: ", err, " stderr: ", stderr)
		cleanup(tarFile, encFile)
		return
	}
	sugar.Info("Created backup archive: ", tarFile)

	encryptCmd := "openssl enc -aes-256-cbc -a -pbkdf2 -iter 600000 -salt -pass pass:\"" +
		backupConfig.EncryptionPassword + "\" -in " + tarFile + " -out " + encFile
	_, stderr, err = shellout(encryptCmd)
	if err != nil {
		sugar.Error("Failed to encrypt backup: ", err, " stderr: ", stderr)
		cleanup(tarFile, encFile)
		return
	}
	sugar.Info("Encrypted backup: ", encFile)

	s3Key := backupConfig.Prefix + "relizacd-workspace-backup-" + timestamp + ".tar.gz.enc"
	if len(backupConfig.Prefix) > 0 && !strings.HasSuffix(backupConfig.Prefix, "/") {
		s3Key = backupConfig.Prefix + "-relizacd-workspace-backup-" + timestamp + ".tar.gz.enc"
	}

	err = uploadToS3(encFile, s3Key)
	if err != nil {
		sugar.Error("Failed to upload backup to S3: ", err)
		cleanup(tarFile, encFile)
		return
	}
	sugar.Info("Backup uploaded to s3://", backupConfig.AwsBucket, "/", s3Key)

	cleanup(tarFile, encFile)
	sugar.Info("Backup completed successfully")
}

func uploadToS3(filePath string, key string) error {
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion(backupConfig.AwsRegion),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				backupConfig.AwsAccessKeyId,
				backupConfig.AwsSecretAccessKey,
				"")))
	if err != nil {
		return err
	}

	client := s3.NewFromConfig(cfg)

	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket: &backupConfig.AwsBucket,
		Key:    &key,
		Body:   file,
	})
	return err
}

func cleanup(files ...string) {
	for _, f := range files {
		os.Remove(f)
	}
}
