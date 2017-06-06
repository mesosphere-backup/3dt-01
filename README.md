# 3DT [![velocity](https://jenkins.mesosphere.com/service/jenkins/buildStatus/icon?job=public-dcos-3dt-pulls)](https://velocity.mesosphere.com/service/jenkins/view/DCOS/job/public-dcos-3dt-pulls/)
## DC/OS Distributed Diagnostics Tool & Aggregation Service
3DT is a monitoring agent which exposes a HTTP API for querying from the /system/health/v1 DC/OS api. 3DT puller collects the data from 3dt agents and represents individual node health for things like system resources as well as DC/OS-specific services.

## Build

```
go get github.com/dcos/3dt
cd $GOPATH/src/github.com/dcos/3dt
make install
./3dt --version
```

## Run
Run 3DT once, on a DC/OS host to check systemd units:

```
3dt --diag
```

Get verbose log output:

```
3dt --diag --verbose
```

Run the 3DT aggregation service to query all cluster hosts for health state:

```
3dt daemon --pull
```

Start the 3DT health API endpoint:

```
3dt daemon
```

### 3DT daemon options

<pre>
--agent-port int
    Use TCP port to connect to agents. (default 1050)

--ca-cert string
    Use certificate authority.

--command-exec-timeout int
    Set command executing timeout (default 120)

--diag
    Get diagnostics output once on the CLI. Does not expose API.

--diagnostics-bundle-dir string
    Set a path to store diagnostic bundles (default "/var/run/dcos/3dt/diagnostic_bundles")

--diagnostics-job-timeout int
    Set a global diagnostics job timeout (default 720)

--diagnostics-units-since string
    Collect systemd units logs since (default "24 hours ago")

--diagnostics-url-timeout int
    Set a local timeout for every single GET request to a log endpoint (default 2)

--endpoint-config string
    Use endpoints_config.json (default "/opt/mesosphere/endpoints_config.json")

--exhibitor-ip string
    Use Exhibitor IP address to discover master nodes. (default "http://127.0.0.1:8181/exhibitor/v1/cluster/status")

--force-tls
    Use HTTPS to do all requests.

--health-update-interval int
    Set update health interval in seconds. (default 60)

--master-port int
    Use TCP port to connect to masters. (default 1050)

--port int
    Web server TCP port. (default 1050)

--pull
    Try to pull checks from DC/OS hosts.

--pull-interval int
    Set pull interval in seconds. (default 60)

--pull-timeout int
    Set pull timeout. (default 3)

--verbose
    Use verbose debug output.

--version
    Print version.
</pre>


## 3dt checks options
TBD

## Test
```
make test
```

Or from any submodule:

```
go test
```

