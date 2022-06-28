package utils

import (
	"encoding/json"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"sync"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	tappv1 "tkestack.io/tapp/pkg/apis/tappcontroller/v1"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	applisters "github.com/clusternet/clusternet/pkg/generated/listers/apps/v1alpha1"
)

const (
	TAppIndexLabel = "TAppIndexLabel"
	TAppAPIVersion = "apps.tkestack.io/v1"
	TAppKind       = "TApp"
)

// TAppIndexList represents a list of consecutive index
type TAppIndexList struct {
	// Start of the list
	Start int32 `json:"start"`
	// End of the list
	End int32 `json:"end"`
}

type TappIndexAndStatuses struct {
	TAPPIndexSetMap map[string]map[string][]TAppIndexList
	Statuses        map[string]map[string]tappv1.InstanceStatus
}

func GenerateTAppIndexAndStatuses(sub *appsapi.Subscription, reservedNamespace string, manifestLister applisters.ManifestLister) (*TappIndexAndStatuses, error) {
	tappIndexAndStatuses := &TappIndexAndStatuses{
		TAPPIndexSetMap: make(map[string]map[string][]TAppIndexList),
		Statuses:        make(map[string]map[string]tappv1.InstanceStatus),
	}
	tAPPIndexSetMapJson, found := sub.Labels[TAppIndexLabel]
	if found {
		if err := json.Unmarshal([]byte(tAPPIndexSetMapJson), &tappIndexAndStatuses.TAPPIndexSetMap); err != nil {
			return nil, err
		}
		wg := sync.WaitGroup{}
		wg.Add(len(sub.Spec.Feeds))
		errCh := make(chan error, len(sub.Spec.Feeds))
		for idx, feed := range sub.Spec.Feeds {
			go func(idx int, feed appsapi.Feed) {
				defer wg.Done()
				if feed.APIVersion != TAppAPIVersion || feed.Kind != TAppKind {
					return
				}

				manifests, err2 := ListManifestsBySelector(reservedNamespace, manifestLister, feed)
				if err2 != nil {
					errCh <- err2
					return
				}
				if manifests == nil {
					errCh <- apierrors.NewNotFound(schema.GroupResource{Resource: feed.Kind}, feed.Name)
					return
				}
				statuses, err3 := getTAppStatuses(manifests[0].Template.Raw)
				if err3 != nil {
					errCh <- err3
					return
				}
				tappIndexAndStatuses.Statuses[GetFeedKey(feed)] = statuses
			}(idx, feed)
		}
		wg.Wait()
		// collect errors
		close(errCh)
		var allErrs []error
		for err3 := range errCh {
			allErrs = append(allErrs, err3)
		}

		if len(allErrs) != 0 {
			return nil, utilerrors.NewAggregate(allErrs)
		}

		return tappIndexAndStatuses, nil
	}

	return nil, nil
}

func getTAppStatuses(rawData []byte) (map[string]tappv1.InstanceStatus, error) {
	tapp := &tappv1.TApp{}

	if err := json.Unmarshal(rawData, tapp); err != nil {
		return nil, err
	}
	return tapp.Spec.Statuses, nil

}

func GenerateTAppStatuses(tappIndexAndStatuses *TappIndexAndStatuses, feedKey, namespacedName string, replicas int32) (map[string]tappv1.InstanceStatus, bool) {
	tappSpecStatuses := make(map[string]tappv1.InstanceStatus)
	tAppIndexSet, ok := tappIndexAndStatuses.TAPPIndexSetMap[feedKey]
	if !ok {
		return tappSpecStatuses, ok
	}
	if len(tAppIndexSet) == 0 {
		return tappSpecStatuses, ok
	}
	indexList, ok := tAppIndexSet[namespacedName]
	if !ok {
		return tappSpecStatuses, ok
	}
	originStatuses, ok := tappIndexAndStatuses.Statuses[feedKey]
	if !ok {
		return tappSpecStatuses, ok
	}

	for k, v := range originStatuses {
		tappSpecStatuses[k] = v
	}

	missIndexStart := int32(0)
	missIndexEnd := int32(0)
	for i := range indexList {
		missIndexEnd = indexList[i].Start
		if missIndexEnd >= replicas {
			missIndexEnd = replicas
		}
		for j := missIndexStart; j < missIndexEnd; j++ {
			tappSpecStatuses[fmt.Sprintf("%d", j)] = tappv1.InstanceKilled
		}
		missIndexStart = indexList[i].End
		if missIndexStart >= replicas {
			break
		}
	}
	missIndexEnd = replicas
	for j := missIndexStart; j < missIndexEnd; j++ {
		tappSpecStatuses[fmt.Sprintf("%d", j)] = tappv1.InstanceKilled
	}
	return tappSpecStatuses, true
}
