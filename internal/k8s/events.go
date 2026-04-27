package k8s

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// eventLister implements EventLister using direct API calls.
type eventLister struct {
	clientset kubernetes.Interface
}

func (e *eventLister) ListEvents(namespace string) ([]Event, error) {
	ctx := context.Background()
	eventList, err := e.clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list events in %s: %w", namespace, err)
	}

	result := make([]Event, 0, len(eventList.Items))
	for _, ev := range eventList.Items {
		result = append(result, Event{
			Type:      ev.Type,
			Reason:    ev.Reason,
			Message:   ev.Message,
			Object:    ev.InvolvedObject.Kind + "/" + ev.InvolvedObject.Name,
			Count:     ev.Count,
			FirstSeen: ev.FirstTimestamp.Time,
			LastSeen:  ev.LastTimestamp.Time,
		})
	}
	return result, nil
}

func (e *eventLister) ListEventsForResource(namespace, kind, name string) ([]Event, error) {
	ctx := context.Background()
	fieldSelector := fmt.Sprintf("involvedObject.kind=%s,involvedObject.name=%s", kind, name)
	eventList, err := e.clientset.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fieldSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("list events for %s/%s in %s: %w", kind, name, namespace, err)
	}

	result := make([]Event, 0, len(eventList.Items))
	for _, ev := range eventList.Items {
		result = append(result, Event{
			Type:      ev.Type,
			Reason:    ev.Reason,
			Message:   ev.Message,
			Object:    ev.InvolvedObject.Kind + "/" + ev.InvolvedObject.Name,
			Count:     ev.Count,
			FirstSeen: ev.FirstTimestamp.Time,
			LastSeen:  ev.LastTimestamp.Time,
		})
	}
	return result, nil
}
