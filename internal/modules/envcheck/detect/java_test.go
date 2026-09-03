package detect

import "testing"

func TestJavaParseDetails(t *testing.T) {
	tests := []struct {
		name, input, vendor, runtime, vm string
	}{
		{
			name:   "temurin",
			input:  "openjdk version \"21.0.2\" 2024-01-16 LTS\nOpenJDK Runtime Environment Temurin-21.0.2+13 (build 21.0.2+13-LTS)\nOpenJDK 64-Bit Server VM Temurin-21.0.2+13 (build 21.0.2+13-LTS, mixed mode, sharing)",
			vendor: "Eclipse Temurin", runtime: "OpenJDK Runtime Environment Temurin-21.0.2+13", vm: "OpenJDK 64-Bit Server VM Temurin-21.0.2+13",
		},
		{
			name:   "corretto",
			input:  "openjdk version \"17.0.12\"\nOpenJDK Runtime Environment Corretto-17.0.12.7.1 (build 17.0.12+7-LTS)\nOpenJDK 64-Bit Server VM Corretto-17.0.12.7.1 (build 17.0.12+7-LTS, mixed mode)",
			vendor: "Amazon Corretto", runtime: "OpenJDK Runtime Environment Corretto-17.0.12.7.1", vm: "OpenJDK 64-Bit Server VM Corretto-17.0.12.7.1",
		},
		{
			name:   "oracle-jdk8",
			input:  "java version \"1.8.0_402\"\nJava(TM) SE Runtime Environment (build 1.8.0_402-b06)\nJava HotSpot(TM) 64-Bit Server VM (build 25.402-b06, mixed mode)",
			vendor: "Oracle", runtime: "Java(TM) SE Runtime Environment", vm: "Java HotSpot(TM) 64-Bit Server VM",
		},
		{
			name:   "adoptopenjdk-openj9",
			input:  "openjdk version \"11.0.11\"\nOpenJDK Runtime Environment AdoptOpenJDK-11.0.11+9 (build 11.0.11+9)\nEclipse OpenJ9 VM AdoptOpenJDK (build openj9-0.26.0)",
			vendor: "Unknown", runtime: "OpenJDK Runtime Environment AdoptOpenJDK-11.0.11+9", vm: "Eclipse OpenJ9 VM AdoptOpenJDK",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			details := (javaDetector{}).ParseDetails(tt.input)
			if details == nil || details.Java == nil {
				t.Fatal("missing Java details")
			}
			if got := details.Java; got.Vendor != tt.vendor || got.Runtime != tt.runtime || got.VM != tt.vm {
				t.Fatalf("details=%+v", got)
			}
		})
	}
}
