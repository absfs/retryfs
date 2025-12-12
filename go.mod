module github.com/absfs/retryfs

go 1.23.0

require (
	github.com/absfs/absfs v0.0.0-20251208232938-aa0ca30de832
	github.com/absfs/memfs v0.0.0-20251208230836-c6633f45580a
	github.com/prometheus/client_golang v1.23.2
	github.com/prometheus/client_model v0.6.2
)

require (
	github.com/absfs/inode v0.0.0-20251208170702-9db24ab95ae4 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/prometheus/common v0.66.1 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	golang.org/x/sys v0.35.0 // indirect
	google.golang.org/protobuf v1.36.8 // indirect
)

replace (
	github.com/absfs/absfs => ../absfs
	github.com/absfs/fstools => ../fstools
	github.com/absfs/inode => ../inode
	github.com/absfs/memfs => ../memfs
)
