/*
 * Created from 'scheme.tl' by 'mtprotoc'
 *
 * Copyright (c) 2021-present,  Teamgram Studio (https://teamgram.io).
 *  All rights reserved.
 *
 * Author: teamgramio (teamgram.io@gmail.com)
 */

package dao

import (
	"context"

	"github.com/teamgram/marmota/pkg/stores/kv"
	"github.com/teamgram/teamgram-server/app/service/dfs/internal/config"
	"github.com/teamgram/teamgram-server/app/service/dfs/internal/minio_util"
	"github.com/teamgram/teamgram-server/app/service/dfs/internal/model"
	idgen_client "github.com/teamgram/teamgram-server/app/service/idgen/client"

	"github.com/zeromicro/go-zero/zrpc"
)

type Dao struct {
	*minio_util.MinioUtil
	idgen_client.IDGenClient2
	ssdb    kv.ExtStore
	parts   *partStore
	persist *persister
}

func New(c config.Config) *Dao {
	d := &Dao{
		MinioUtil:    minio_util.MustNewMinioClient(&c.Minio),
		IDGenClient2: idgen_client.NewIDGenClient2(zrpc.MustNewClient(c.IdGen)),
		ssdb:         kv.NewStore(c.SSDB),
		parts:        newPartStore(c.Parts),
	}
	d.persist = newPersister(d.putObject)
	return d
}

func NewDFSHelper(minio *minio_util.MinioConfig, idgen zrpc.RpcClientConf, ssdb kv.KvConf, parts model.PartsConf) *Dao {
	d := &Dao{
		MinioUtil:    minio_util.MustNewMinioClient(minio),
		IDGenClient2: idgen_client.NewIDGenClient2(zrpc.MustNewClient(idgen)),
		ssdb:         kv.NewStore(ssdb),
		parts:        newPartStore(parts),
	}
	d.persist = newPersister(d.putObject)
	return d
}

// SweepParts resumes the stores left unfinished, then removes idle uploads
// until ctx ends (task 057).
func (d *Dao) SweepParts(ctx context.Context) {
	d.ResumeStores(ctx)
	d.parts.runSweeper(ctx)
}

func NewMinioHelper(minio *minio_util.MinioConfig) *minio_util.MinioUtil {
	return minio_util.MustNewMinioClient(minio)
}
