package discovery

import "testing"

func TestParseServiceInventory(t *testing.T) {
	output := `HOSTNAME_BEGIN
db01
HOSTNAME_END
PORTS_BEGIN
tcp LISTEN 0 128 0.0.0.0:3306 0.0.0.0:* users:(("mysqld",pid=100,fd=20))
tcp LISTEN 0 128 0.0.0.0:9104 0.0.0.0:* users:(("mysqld_exporter",pid=101,fd=3))
tcp LISTEN 0 128 0.0.0.0:6379 0.0.0.0:* users:(("redis-server",pid=200,fd=6))
PORTS_END
PROCESS_BEGIN
100 mysql mysqld /usr/sbin/mysqld --datadir=/var/lib/mysql --log-error=/var/log/mysql/error.log
101 mysql mysqld_exporter /usr/local/bin/mysqld_exporter
200 redis redis-server /usr/bin/redis-server 0.0.0.0:6379
PROCESS_END`
	services := parseServiceInventory("10.0.0.10", "db01", output)
	if len(services) != 2 {
		t.Fatalf("expected 2 services, got %#v", services)
	}
	byType := map[string]discoveredService{}
	for _, item := range services {
		byType[item.Subtype] = item
	}
	if byType["mysql"].Port != 3306 || byType["mysql"].DataDir != "/var/lib/mysql" || byType["mysql"].ExporterPort != "9104" {
		t.Fatalf("unexpected mysql service: %#v", byType["mysql"])
	}
	if byType["redis"].Port != 6379 || byType["redis"].User != "redis" {
		t.Fatalf("unexpected redis service: %#v", byType["redis"])
	}
}

func TestParseServiceInventoryKafkaJVM(t *testing.T) {
	output := `HOSTNAME_BEGIN
mq01
HOSTNAME_END
PORTS_BEGIN
tcp LISTEN 0 128 0.0.0.0:9092 0.0.0.0:* users:(("java",pid=300,fd=30))
PORTS_END
PROCESS_BEGIN
300 kafka java -Dcom.sun.management.jmxremote.port=9999 kafka.Kafka --override log.dirs=/data/kafka
PROCESS_END`
	services := parseServiceInventory("10.0.0.11", "mq01", output)
	if len(services) != 1 || services[0].Subtype != "kafka" || services[0].Port != 9092 || services[0].JVMPort != "9999" || services[0].DataDir != "/data/kafka" {
		t.Fatalf("unexpected kafka service: %#v", services)
	}
}
func TestExpandCIDRSupportsSingleIP(t *testing.T) {
	items, err := expandCIDR("172.28.69.101")
	if err != nil || len(items) != 1 || items[0] != "172.28.69.101" {
		t.Fatalf("expand single IP: %#v %v", items, err)
	}
}
