# Exported metrics

## `imageregistry:operations_count:sum`

| Operation | Resource type      |
| --------- | ------------------ |
| `get`     | `blob`, `manifest` |
| `create`  | `blob`, `manifest` |

## `imageregistry:imagestreamtags_count:sum`

| Source     | Location             | Description                                                   |
| ---------- | -------------------- | ------------------------------------------------------------- |
| `imported` | `openshift`, `other` | Image Stream Tags imported in 'openshift' or other namespaces |
| `pushed`   | `openshift`, `other` | Image Stream Tags pushed to 'openshift' or other namespaces   |
