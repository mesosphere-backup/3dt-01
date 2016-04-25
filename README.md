# 3DT
## DC/OS Distributed Diagnostics Tool & Aggregation Service
3DT is a monitoring agent which exposes a HTTP API for querying from the /system/health/v1 DC/OS api. 3DT puller collects the data from 3dt agents and represents individual node health for things like system resources as well as DC/OS-specific services.

## Build

```
go get github.com/dcos/3dt
cd $GOPATH/src/github.com/dcos/3dt
go install
3dt -version
```

## Run
Run 3DT once, on a DC/OS host to check systemd units:

```
3dt -diag
```

Get verbose log output:

```
3dt -diag -verbose
```

Run the 3DT aggregation service to query all cluster hosts for health state:

```
3dt -pull
```

Start the 3DT health API endpoint:

```
3dt
```

### 3DT CLI Arguments

<pre>
-verbose        | Verbose Logging.
-diag           | Diagnostics mode. This will execute health checks once, return the output and exit.
-version        | Print version and exit.
-pull           | Run the aggregation service and expose the /system/health/v1/* api.
-port           | Web server TCP port. (default 1050)
-pull-interval  | Set pull interval, default 60 sec.
</pre>

## Test
```
ginkgo -r -trace -race -randomizeAllSpecs -randomizeSuites -cover
```

Or from any submodule:

```
go test
```

or

```
ginkgo
```

