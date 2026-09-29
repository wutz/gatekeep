package policy

import (
	"strings"
	"testing"
)

func TestDefaultPolicy(t *testing.T) {
	p, err := Load("../../configs/policy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		cmd  string
		want Level
	}{
		{"uptime", L0Read},
		{"df -h", L0Read},
		{"journalctl -u kubelet -n 200", L0Read},
		{"journalctl --vacuum-time=1d", L2High},
		{"dmesg -T", L0Read},
		{"dmesg -c", L2High},
		{"ip addr", L0Read},
		{"ip -s link show", L0Read},
		{"ip link set eth0 down", L2High},
		{"find /var/log -name *.log", L0Read},
		{"find /tmp -delete", L2High},
		{"cat /etc/shadow", L2High},
		{"cat /var/log/messages", L0Read},
		{"sysctl vm.swappiness", L0Read},
		{"sysctl -w vm.swappiness=10", L2High},
		{"systemctl status kubelet", L0Read},
		{"systemctl restart kubelet", L1Low},
		{"systemctl stop kubelet", L2High},
		{"kubectl get pods -A", L0Read},
		{"kubectl logs -n kube-system coredns-x", L0Read},
		{"kubectl get secret -n default", L2High},
		{"kubectl delete pod foo", L1Low},
		{"kubectl delete deploy foo", L2High},
		{"kubectl delete ns prod", L3Critical},
		{"kubectl apply -f x.yaml", L2High},
		{"ceph -s", L0Read},
		{"ceph osd tree", L0Read},
		{"ceph auth ls", L2High},
		{"ceph osd set noout", L1Low},
		{"ceph osd out 3", L2High},
		{"ceph osd pool delete rbd rbd --yes-i-really-really-mean-it", L3Critical},
		{"mmlscluster", L0Read},
		{"mmgetstate -a", L0Read},
		{"mmhealth node show", L0Read},
		{"mmchconfig pagepool=4G", L2High},
		{"mmmount fs1 -a", L1Low},
		{"mmdelfs fs1", L3Critical},
		{"rm -rf /", L3Critical},
		{"rm foo", L2High},
		{"some-unknown-tool", L2High},
		{"cat /etc/hosts;rm", L3Critical},
		{"/usr/bin/uptime", L0Read},
	}
	for _, c := range cases {
		d, err := p.Classify(strings.Fields(c.cmd), "")
		if err != nil {
			t.Fatalf("%s: %v", c.cmd, err)
		}
		if d.Level != c.want {
			t.Errorf("%q: got %s (rule %s), want %s", c.cmd, d.Level, d.Rule, c.want)
		}
	}
}

func TestExpandBraces(t *testing.T) {
	got := expandBraces("{a,b}x")
	if len(got) != 2 || got[0] != "ax" || got[1] != "bx" {
		t.Fatal(got)
	}
}
