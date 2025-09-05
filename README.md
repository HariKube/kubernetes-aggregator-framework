# kubernetes-aggregator-framework
Helper framework to make Kubernetes Aggregation API development easier

## What the Kubernetes Aggregation Layer Gives You

The [Kubernetes Aggregation Layer](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/apiserver-aggregation/) allows you to extend the core Kubernetes API with your own custom APIs. Think of it as a gateway that lets you add new types of resources to your cluster that behave just like native Kubernetes objects (like pods or services).

## The Difficulty of Developing an API Server Without Frameworks

Building a custom API server for Kubernetes from scratch is a complex and challenging task. It requires a deep understanding of the Kubernetes API's internals, including:

This level of detail makes it very difficult to develop an API server without extensive, in-depth knowledge of Kubernetes' inner workings.

## How This Framework Simplifies the Process

Recognizing these challenges, our framework provides a straightforward solution to make building custom Kubernetes API servers easy and accessible. It hides away much of the complex, low-level work, allowing you to focus on your core logic.

## How to Use It

Creating raw endpoints, without any Kubernetes behaviour dependency.

```golang
Server: *kaf.NewServer(kaf.ServerConfig{
    Port:     port,
    CertFile: certFile,
    KeyFile:  keyFile,
    Group:    "example.com",
    Version:  "v1",
    APIKinds: []kaf.APIKind{
        {
            ApiResource: metav1.APIResource{
                Name:  "foo",
                Verbs: []string{"get"},
            },
            RawEndpoints: map[string]http.HandlerFunc{
                "": func(w http.ResponseWriter, r *http.Request) {
                    ...
                },
                "/bar": func(w http.ResponseWriter, r *http.Request) {
                    ...
                },
            },
        },
    },
}),
```

Call API via `kubectl`.

```bash
kubectl get --raw /apis/example.com/v1/foo
kubectl get --raw /apis/example.com/v1/foo/bar
```

---

Create fully customized API endpoints for cluster scoped `clustertasks` and namespace scoped `customtasks`.

```golang
kaf.APIKind {
    ApiResource: metav1.APIResource{
        Name:  "clustertasks",
        Kind:  "ClusterPod",
        Verbs: []string{"get", "list", "watch", "create", "update", "delete"},
    },
    CustomResources: []kaf.CustomResource{
        {
            CreateHandler: func(namespace, name string, w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "application/json; charset=utf-8")
            },
            GetHandler: func(namespace, name string, w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "application/json; charset=utf-8")
            },
            ListHandler: func(namespace, name string, w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "application/json; charset=utf-8")
            },
            ReplaceHandler: func(namespace, name string, w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "application/json; charset=utf-8")
            },
            DeleteHandler: func(namespace, name string, w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "application/json; charset=utf-8")
            },
            WatchHandler: func(namespace, name string, w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "application/json; charset=utf-8")
            },
        },
    },
},
kaf.APIKind {
    ApiResource: metav1.APIResource{
        Name:       "customtasks",
        Namespaced: true,
        Kind:       "CustomPod",
        Verbs:      []string{"get", "list", "watch", "create", "update", "delete"},
    },
    CustomResources: []kaf.CustomResource{
        {
            CreateHandler: func(namespace, name string, w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "application/json; charset=utf-8")
            },
            GetHandler: func(namespace, name string, w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "application/json; charset=utf-8")
            },
            ListHandler: func(namespace, name string, w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "application/json; charset=utf-8")
            },
            ReplaceHandler: func(namespace, name string, w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "application/json; charset=utf-8")
            },
            DeleteHandler: func(namespace, name string, w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "application/json; charset=utf-8")
            },
            WatchHandler: func(namespace, name string, w http.ResponseWriter, r *http.Request) {
                w.Header().Set("Content-Type", "application/json; charset=utf-8")
            },
        },
    },
},
```

Call API via `kubectl`.

```bash
kubectl create --raw /apis/example.com/v1/clustertasks/foo -f clustertask.yaml
kubectl replace --raw /apis/example.com/v1/clustertasks/foo -f clustertask.yaml
kubectl get clustertasks
kubectl get clustertasks foo
kubectl get clustertasks -w
kubectl get clustertasks foo -w
kubectl delete clustertasks foo
```

```bash
kubectl create --raw /apis/example.com/v1/namespaces/default/customtasks/foo -f customtask.yaml
kubectl replace --raw /apis/example.com/v1/namespaces/default/customtasks/foo -f clustertask.yaml
kubectl get customtasks
kubectl get customtasks foo
kubectl get customtasks -w
kubectl get customtasks foo -w
kubectl delete customtasks foo
```

---

Create and API extending Kubernetes API capabilities, for example collecting events of `Pod`s.

```golang
type CombinedPod struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   corev1.PodSpec    `json:"spec,omitempty"`
	Status corev1.PodStatus  `json:"status,omitempty"`
}

type CombinedPodList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CombinedPod `json:"items"`
}
```

```golang
kubeClient, _ := client.New(ctrl.GetConfigOrDie(), client.Options{
  Scheme: scheme,
})

dynamicKubeClient, _ := dynamic.NewForConfig(ctrl.GetConfigOrDie())

Server: *kaf.NewServer(kaf.ServerConfig{
    KubeClient: kubeClient,
    DynamicKubeClient: dynamicKubeClient,
    ...
```

```golang
kaf.APIKind {
  ApiResource: metav1.APIResource{
    Name:       "combinedpods",
    Namespaced: true,
    Kind:       "CombinedPod",
    Verbs:      []string{"get", "list", "watch"},
  },
  Resources: []kaf.Resource{
    {
      CreateNew: func() (schema.GroupVersionResource, client.Object) {
        return corev1.GroupVersion.WithResource("pods"), &corev1.Pod{}
      },
      CreateNewList: func() (schema.GroupVersionResource, client.ObjectList) {
        return corev1.GroupVersion.WithResource("podlist"), &corev1.PodList{}
      },
      ListCallback: func(ctx context.Context, namespace, _ string, objList client.ObjectList) (any, error) {
        podList, ok := objList.(*corev1.PodList)
        if !ok {
          return nil, fmt.Errorf("failed to convert podlist for: %s", objList.GetObjectKind().GroupVersionKind().String())
        }

        // Do what you want

        combinedPods := CombinedPodList{
          TypeMeta: metav1.TypeMeta{
            Kind:       "CombinedPodList",
            APIVersion: Group + "/" + Version,
          },
          ListMeta: metav1.ListMeta{
            ResourceVersion:    podList.ResourceVersion,
            Continue:           podList.Continue,
            RemainingItemCount: podList.RemainingItemCount,
          },
          Items: []CombinedPod{},
        }

        for _, t := range items {
          pod := t.(*corev1.Pod)

          ct := CombinedPod{
            TypeMeta: metav1.TypeMeta{
              Kind:       "CombinedPod",
              APIVersion: Group + "/" + Version,
            },
            ObjectMeta: pod.ObjectMeta,
            Spec:       pod.Spec,
          }

          combinedPods.Items = append(combinedPods.Items, ct)
        }

        return combinedPods, nil
      },
      WatchCallback: func(ctx context.Context, _, _ string, unstructuredObj *unstructured.Unstructured) (any, error) {
        pod := corev1.Pod{}
        if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredObj.Object, &pod); err != nil {
          return nil, fmt.Errorf("failed to convert unstructured: %v", err)
        }

        // Do what you want

        cp := CombinedPod{
          TypeMeta: metav1.TypeMeta{
            Kind:       "CombinedPod",
            APIVersion: Group + "/" + Version,
          },
          ObjectMeta: pod.ObjectMeta,
          Spec:       pod.Spec,
        }

        return cp, nil
      },
    },
  },
},
```

Call API via `kubectl`.

```bash
kubectl get combinedtasks -A
kubectl get combinedtasks -n default
kubectl get combinedtasks -n default foo
kubectl get combinedtasks -n default foo -o yaml
```

## 🤝 Contribution Guide

We welcome and encourage contributions from the community! Whether it's a bug fix, a new feature, or an improvement to the documentation, your help is greatly appreciated.

Before you get started, please take a moment to review our guidelines:

- Read the Documentation: Familiarize yourself with the framework's architecture and existing features.
- Open an Issue: For any significant changes or new features, please open an issue first to discuss the idea. This helps prevent duplicated work and ensures alignment with the project's goals.
- Fork the Repository: Fork the repository to your own GitHub account.
- Create a Branch: Create a new branch for your feature or bug fix: git checkout -b feature-my-awesome-feature.
- Commit Your Changes: Make your changes and commit them with a clear and descriptive message.
- Submit a Pull Request: Push your branch to your forked repository and open a pull request against the main branch of this repository. Please provide a clear description of your changes in the PR.

We are committed to providing a friendly, safe, and welcoming environment for all, regardless of background or experience. We are following Kubernetes Please see them [Code of Conduct](https://kubernetes.io/community/code-of-conduct/) for more details.

## 🙏 Share Feedback and Report Issues

Your feedback is invaluable in helping us improve this framework. If you encounter any issues, have a suggestion for a new feature, or simply want to share your experience, we want to hear from you!

- Report Bugs: If you find a bug, please open a [GitHub Issue](https://github.com/mhmxs/kubernetes-aggregator-framework/issues). Include as much detail as possible, such as steps to reproduce the bug, expected behavior, and your environment (e.g., Kubernetes version, Go version).
- Request a Feature: If you have an idea for a new feature, open a [GitHub Issue](https://github.com/mhmxs/kubernetes-aggregator-framework/issues) and use the feature request label. Describe the use case and how the new feature would benefit the community.
- Ask a Question: For general questions or discussions, please use the [GitHub Discussions](https://github.com/mhmxs/kubernetes-aggregator-framework/discussions).

## 📝 License

This project is licensed under the BSD 3-Clause "New" or "Revised" License. See the LICENSE file for details.

## ✨ Special Thanks

We'd like to extend our gratitude to the Kubernetes community and the developers of related projects like controller-runtime and kubebuilder for their foundational work that inspired and enabled the creation of this framework.