package javaparser

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/afero"

	"github.com/stretchr/testify/require"
)

// TestResolveAppServerFromCmdLine tests that vendor can be determined from the process cmdline
func TestResolveAppServerFromCmdLine(t *testing.T) {
	tests := []struct {
		name           string
		rawCmd         string
		expectedVendor serverVendor
		expectedHome   string
	}{
		{
			name: "wildfly 18 standalone",
			rawCmd: `/home/app/.sdkman/candidates/java/17.0.4.1-tem/bin/java -D[Standalone] -server
-Xms64m -Xmx512m -XX:MetaspaceSize=96M -XX:MaxMetaspaceSize=256m -Djava.net.preferIPv4Stack=true
-Djboss.modules.system.pkgs=org.jboss.byteman -Djava.awt.headless=true
--add-exports=java.base/sun.nio.ch=ALL-UNNAMED --add-exports=jdk.unsupported/sun.misc=ALL-UNNAMED
--add-exports=jdk.unsupported/sun.reflect=ALL-UNNAMED -Dorg.jboss.boot.log.file=/home/app/Downloads/wildfly-18.0.0.Final/standalone/log/server.log
-Dlogging.configuration=file:/home/app/Downloads/wildfly-18.0.0.Final/standalone/configuration/logging.properties
-jar /home/app/Downloads/wildfly-18.0.0.Final/jboss-modules.jar -mp /home/app/Downloads/wildfly-18.0.0.Final/modules org.jboss.as.standalone
-Djboss.home.dir=/home/app/Downloads/wildfly-18.0.0.Final -Djboss.server.base.dir=/home/app/Downloads/wildfly-18.0.0.Final/standalone`,
			expectedVendor: jboss,
			expectedHome:   "/home/app/Downloads/wildfly-18.0.0.Final",
		},
		{
			name: "wildfly 18 domain",
			rawCmd: `/home/app/.sdkman/candidates/java/17.0.4.1-tem/bin/java --add-exports=java.base/sun.nio.ch=ALL-UNNAMED
--add-exports=jdk.unsupported/sun.reflect=ALL-UNNAMED --add-exports=jdk.unsupported/sun.misc=ALL-UNNAMED -D[Server:server-one]
-D[pcid:780891833] -Xms64m -Xmx512m -server -XX:MetaspaceSize=96m -XX:MaxMetaspaceSize=256m -Djava.awt.headless=true -Djava.net.preferIPv4Stack=true
-Djboss.home.dir=/home/app/Downloads/wildfly-18.0.0.Final -Djboss.modules.system.pkgs=org.jboss.byteman
-Djboss.server.log.dir=/home/app/Downloads/wildfly-18.0.0.Final/domain/servers/server-one/log
-Djboss.server.temp.dir=/home/app/Downloads/wildfly-18.0.0.Final/domain/servers/server-one/tmp
-Djboss.server.data.dir=/home/app/Downloads/wildfly-18.0.0.Final/domain/servers/server-one/data
-Dorg.jboss.boot.log.file=/home/app/Downloads/wildfly-18.0.0.Final/domain/servers/server-one/log/server.log
-Dlogging.configuration=file:/home/app/Downloads/wildfly-18.0.0.Final/domain/configuration/default-server-logging.properties
-jar /home/app/Downloads/wildfly-18.0.0.Final/jboss-modules.jar -mp /home/app/Downloads/wildfly-18.0.0.Final/modules org.jboss.as.server`,
			expectedVendor: jboss,
			expectedHome:   "/home/app/Downloads/wildfly-18.0.0.Final",
		},
		{
			name: "tomcat 10.x",
			rawCmd: `java -Djava.util.logging.config.file=/app/Code/tomcat/apache-tomcat-10.0.27/conf/logging.properties
-Djava.util.logging.manager=org.apache.juli.ClassLoaderLogManager -Djdk.tls.ephemeralDHKeySize=2048
-Djava.protocol.handler.pkgs=org.apache.catalina.webresources -Dorg.apache.catalina.security.SecurityListener.UMASK=0027
-Dignore.endorsed.dirs= -classpath /app/Code/tomcat/apache-tomcat-10.0.27/bin/bootstrap.jar:/app/Code/tomcat/apache-tomcat-10.0.27/bin/tomcat-juli.jar
-Dcatalina.base=/app/Code/tomcat/apache-tomcat-10.0.27/myserver -Dcatalina.home=/app/Code/tomcat/apache-tomcat-10.0.27
-Djava.io.tmpdir=/app/Code/tomcat/apache-tomcat-10.0.27/temp org.apache.catalina.startup.Bootstrap start`,
			expectedVendor: tomcat,
			expectedHome:   "/app/Code/tomcat/apache-tomcat-10.0.27/myserver",
		},
		{
			name: "weblogic 12",
			rawCmd: `/u01/jdk/bin/java -Djava.security.egd=file:/dev/./urandom -cp /u01/oracle/wlserver/server/lib/weblogic-launcher.jar
-Dlaunch.use.env.classpath=true -Dweblogic.Name=AdminServer -Djava.security.policy=/u01/oracle/wlserver/server/lib/weblogic.policy
-Djava.system.class.loader=com.oracle.classloader.weblogic.LaunchClassLoader -javaagent:/u01/oracle/wlserver/server/lib/debugpatch-agent.jar
-da -Dwls.home=/u01/oracle/wlserver/server -Dweblogic.home=/u01/oracle/wlserver/server weblogic.Server`,
			expectedVendor: weblogic,
		},
		{
			name: "websphere",
			rawCmd: `/opt/java/openjdk/bin/java -javaagent:/opt/ol/wlp/bin/tools/ws-javaagent.jar -Djava.awt.headless=true
-Djdk.attach.allowAttachSelf=true --add-exportsjava.base/sun.security.action=ALL-UNNAMED --add-exportsjava.naming/com.sun.jndi.ldap=ALL-UNNAMED
--add-exportsjava.naming/com.sun.jndi.url.ldap=ALL-UNNAMED --add-exportsjdk.naming.dns/com.sun.jndi.dns=ALL-UNNAMED
--add-exportsjava.security.jgss/sun.security.krb5.internal=ALL-UNNAMED --add-exportsjdk.attach/sun.tools.attach=ALL-UNNAMED
--add-opensjava.base/java.util=ALL-UNNAMED --add-opensjava.base/java.lang=ALL-UNNAMED --add-opensjava.base/java.util.concurrent=ALL-UNNAMED
--add-opensjava.base/java.io=ALL-UNNAMED --add-opensjava.naming/javax.naming.spi=ALL-UNNAMED --add-opensjdk.naming.rmi/com.sun.jndi.url.rmi=ALL-UNNAMED
--add-opensjava.naming/javax.naming=ALL-UNNAMED --add-opensjava.rmi/java.rmi=ALL-UNNAMED --add-opensjava.sql/java.sql=ALL-UNNAMED
--add-opensjava.management/javax.management=ALL-UNNAMED --add-opensjava.base/java.lang.reflect=ALL-UNNAMED --add-opensjava.desktop/java.awt.image=ALL-UNNAMED
--add-opensjava.base/java.security=ALL-UNNAMED --add-opensjava.base/java.net=ALL-UNNAMED --add-opensjava.base/java.text=ALL-UNNAMED
--add-opensjava.base/sun.net.www.protocol.https=ALL-UNNAMED --add-exportsjdk.management.agent/jdk.internal.agent=ALL-UNNAMED
--add-exportsjava.base/jdk.internal.vm=ALL-UNNAMED -jar /opt/ol/wlp/bin/tools/ws-server.jar defaultServer`,
			expectedHome:   "",
			expectedVendor: websphere,
		},
		{
			// weblogic cli have the same system properties than normal weblogic server run (sourced from setWlsEnv.sh)
			// however, the main entry point changes (weblogic.Deployer) hence should be recognized as unknown
			name: "weblogic deployer",
			rawCmd: `/u01/jdk/bin/java -Djava.security.egd=file:/dev/./urandom -cp /u01/oracle/wlserver/server/lib/weblogic-launcher.jar
-Dlaunch.use.env.classpath=true -Dweblogic.Name=AdminServer -Djava.security.policy=/u01/oracle/wlserver/server/lib/weblogic.policy
-Djava.system.class.loader=com.oracle.classloader.weblogic.LaunchClassLoader -javaagent:/u01/oracle/wlserver/server/lib/debugpatch-agent.jar
-da -Dwls.home=/u01/oracle/wlserver/server -Dweblogic.home=/u01/oracle/wlserver/server weblogic.Deployer -upload -target myserver -deploy some.war`,
			expectedVendor: unknown,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vendor, home := resolveAppServerFromCmdLine(strings.Split(strings.ReplaceAll(tt.rawCmd, "\n", " "), " "))
			require.Equal(t, tt.expectedVendor, vendor)
			// the base dir is making sense only when the vendor has been properly understood
			if tt.expectedVendor != unknown {
				require.Equal(t, tt.expectedHome, home)
			}
		})
	}
}

// TestExtractContextRootFromApplicationXml tests that context root can be extracted from an ear under /META-INF/application.xml
func TestExtractContextRootFromApplicationXml(t *testing.T) {
	tests := []struct {
		name     string
		xml      string
		expected []string
		err      bool
	}{
		{
			name: "application.xml with webapps",
			xml: `<application xmlns="http://xmlns.jcp.org/xml/ns/javaee" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
xsi:schemaLocation="http://xmlns.jcp.org/xml/ns/javaee http://xmlns.jcp.org/xml/ns/javaee/application_7.xsd" version="7">
	<application-name>myapp</application-name>
	<initialize-in-order>false</initialize-in-order>
  	<module><ejb>mymodule.jar</ejb></module>
  <module>
        <web>
            <web-uri>myweb1.war</web-uri>
            <context-root>MyWeb1</context-root>
        </web>
    </module>
	<module>
        <web>
            <web-uri>myweb2.war</web-uri>
            <context-root>MyWeb2</context-root>
        </web>
    </module>
</application>`,
			expected: []string{"MyWeb1", "MyWeb2"},
		},
		{
			name: "application.xml with doctype and no webapps",
			xml: `<!DOCTYPE application PUBLIC "-//Sun Microsystems, Inc.//DTD J2EE Application 1.2//EN
http://java.sun.com/j2ee/dtds/application_1_2.dtd">
<application><module><java>my_app.jar</java></module></application>`,
			expected: nil,
		},
		{
			name: "no application.xml (invalid ear)",
			err:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mapFs := afero.NewMemMapFs()

			if len(tt.xml) > 0 {
				require.NoError(t, afero.WriteFile(mapFs, applicationXMLPath, []byte(tt.xml), 0664))
			}
			value, err := extractContextRootFromApplicationXML(mapFs)
			if tt.err {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.expected, value)
			}
		})
	}
}

// TestWeblogicExtractServiceNamesForJEEServer tests all cases of detecting weblogic as vendor and extracting context root.
// It simulates having 1 ear deployed, 1 war with weblogic.xml and 1 war without weblogic.xml.
// Hence, it should extract ear context from application.xml, 1st war context from weblogic.xml and derive last war context from the filename.
func TestWeblogicExtractServiceNamesForJEEServer(t *testing.T) {
	wlsConfig := `
<domain>
    <app-deployment>
        <target>AdminServer</target>
        <source-path>/apps/app1.ear</source-path>
        <staging-mode>stage</staging-mode>
    </app-deployment>
    <app-deployment>
        <target>AdminServer</target>
        <source-path>/apps/app2.war</source-path>
        <staging-mode>stage</staging-mode>
    </app-deployment>
    <app-deployment>
        <target>AdminServer</target>
        <source-path>/apps/app3.war</source-path>
        <staging-mode>stage</staging-mode>
    </app-deployment>
</domain>`
	appXML := `
<application>
  <application-name>myapp</application-name>
  <initialize-in-order>false</initialize-in-order>
  <module>
	<web>
      <web-uri>app1.war</web-uri>
      <context-root>app1_context</context-root>
    </web>
  </module>
</application>`
	weblogicXML := `
<weblogic-web-app>
   <context-root>app2_context</context-root>
</weblogic-web-app>
`
	memfs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(memfs, "/wls/domain/config/config.xml", []byte(wlsConfig), 0664))
	require.NoError(t, afero.WriteFile(memfs, "/apps/app1.ear"+applicationXMLPath, []byte(appXML), 0664))
	buf := bytes.NewBuffer([]byte{})
	writer := zip.NewWriter(buf)
	require.NoError(t, writeFile(writer, weblogicXMLFile, weblogicXML))
	require.NoError(t, writer.Close())
	require.NoError(t, afero.WriteFile(memfs, "/apps/app2.war", buf.Bytes(), 0664))
	require.NoError(t, memfs.MkdirAll("/apps/app3.war", 0775))

	// simulate weblogic command line args
	cmd := []string{
		wlsServerNameSysProp + "AdminServer",
		wlsHomeSysProp + "/wls",
		wlsServerMainClass,
	}
	extractedContextRoots := ExtractServiceNamesForJEEServer(cmd, "/wls/domain", memfs)
	require.Equal(t, []string{
		"app1_context", // taken from ear application.xml
		"app2_context", // taken from war weblogic.xml
		"app3",         // derived from the war filename
	}, extractedContextRoots)
}
