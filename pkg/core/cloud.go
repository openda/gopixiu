/*
Copyright 2021 The Pixiu Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package core

import (
	"context"
	"fmt"

	v1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/caoyingjunz/gopixiu/api/types"
	"github.com/caoyingjunz/gopixiu/pkg/core/client"
	"github.com/caoyingjunz/gopixiu/pkg/db"
	"github.com/caoyingjunz/gopixiu/pkg/db/model"
	"github.com/caoyingjunz/gopixiu/pkg/log"
)

var clientError = fmt.Errorf("failed to found clout client")

type CloudGetter interface {
	Cloud() CloudInterface
}

type CloudInterface interface {
	Create(ctx context.Context, obj *types.Cloud) error
	Update(ctx context.Context, obj *types.Cloud) error
	Delete(ctx context.Context, cid int64) error
	Get(ctx context.Context, cid int64) (*types.Cloud, error)
	List(ctx context.Context) ([]types.Cloud, error)

	InitCloudClients() error

	DeleteDeployment(ctx context.Context, deleteOptions types.GetOrDeleteOptions) error
	ListDeployments(ctx context.Context, listOptions types.ListOptions) ([]v1.Deployment, error)

	ListNamespaces(ctx context.Context, cloudName string) ([]corev1.Namespace, error)

	ListJobs(ctx context.Context, listOptions types.ListOptions) ([]batchv1.Job, error)
}

var clientSets client.ClientsInterface

type cloud struct {
	app     *pixiu
	factory db.ShareDaoFactory
}

func newCloud(c *pixiu) CloudInterface {
	return &cloud{
		app:     c,
		factory: c.factory,
	}
}

func (c *cloud) preCreate(ctx context.Context, obj *types.Cloud) error {
	if len(obj.Name) == 0 {
		return fmt.Errorf("invalid empty cloud name")
	}
	if len(obj.KubeConfig) == 0 {
		return fmt.Errorf("invalid empty kubeconfig data")
	}

	return nil
}

func (c *cloud) Create(ctx context.Context, obj *types.Cloud) error {
	if err := c.preCreate(ctx, obj); err != nil {
		log.Logger.Errorf("failed to pre-check for %a created: %v", obj.Name, err)
		return err
	}

	// 先构造 clientSet，如果有异常，直接返回
	clientSet, err := c.newClientSet(obj.KubeConfig)
	if err != nil {
		log.Logger.Errorf("failed to create %s clientSet: %v", obj.Name, err)
		return err
	}
	if _, err = c.factory.Cloud().Create(ctx, &model.Cloud{
		Name:       obj.Name,
		KubeConfig: string(obj.KubeConfig),
	}); err != nil {
		log.Logger.Errorf("failed to create %s cloud: %v", obj.Name, err)
		return err
	}

	clientSets.Add(obj.Name, clientSet)
	return nil
}

func (c *cloud) Update(ctx context.Context, obj *types.Cloud) error { return nil }

func (c *cloud) Delete(ctx context.Context, cid int64) error {
	// TODO: 删除cloud的同时，直接返回，避免一次查询
	obj, err := c.factory.Cloud().Get(ctx, cid)
	if err != nil {
		log.Logger.Errorf("failed to get %s cloud: %v", cid, err)
		return err
	}
	if err = c.factory.Cloud().Delete(ctx, cid); err != nil {
		log.Logger.Errorf("failed to delete %s cloud: %v", cid, err)
		return err
	}

	clientSets.Delete(obj.Name)
	return nil
}

func (c *cloud) Get(ctx context.Context, cid int64) (*types.Cloud, error) {
	cloudObj, err := c.factory.Cloud().Get(ctx, cid)
	if err != nil {
		log.Logger.Errorf("failed to get %d cloud: %v", cid, err)
		return nil, err
	}

	return c.model2Type(cloudObj), nil
}

func (c *cloud) List(ctx context.Context) ([]types.Cloud, error) {
	cloudObjs, err := c.factory.Cloud().List(ctx)
	if err != nil {
		log.Logger.Errorf("failed to list clouds: %v", err)
		return nil, err
	}
	var cs []types.Cloud
	for _, cloudObj := range cloudObjs {
		cs = append(cs, *c.model2Type(&cloudObj))
	}

	return cs, nil
}

func (c *cloud) InitCloudClients() error {
	// 初始化云客户端
	clientSets = client.NewCloudClients()

	cloudObjs, err := c.factory.Cloud().List(context.TODO())
	if err != nil {
		log.Logger.Errorf("failed to list exist clouds: %v", err)
		return err
	}
	for _, cloudObj := range cloudObjs {
		clientSet, err := c.newClientSet([]byte(cloudObj.KubeConfig))
		if err != nil {
			log.Logger.Errorf("failed to create %s clientSet: %v", cloudObj.Name, err)
			return err
		}
		clientSets.Add(cloudObj.Name, clientSet)
	}

	return nil
}

func (c *cloud) newClientSet(data []byte) (*kubernetes.Clientset, error) {
	kubeConfig, err := clientcmd.RESTConfigFromKubeConfig(data)
	if err != nil {
		return nil, err
	}

	return kubernetes.NewForConfig(kubeConfig)
}

func (c *cloud) model2Type(obj *model.Cloud) *types.Cloud {
	return &types.Cloud{
		Id:          obj.Id,
		Name:        obj.Name,
		Status:      obj.Status,
		Description: obj.Description,
		TimeSpec: types.TimeSpec{
			GmtCreate:   obj.GmtCreate.Format(timeLayout),
			GmtModified: obj.GmtModified.Format(timeLayout),
		},
	}
}
