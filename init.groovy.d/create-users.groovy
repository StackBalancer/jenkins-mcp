import jenkins.model.*
import hudson.security.*
import jenkins.security.ApiTokenProperty
import java.nio.file.Files
import java.nio.file.Paths

def instance = Jenkins.getInstanceOrNull()
if (!instance) return

// --- Read env vars ---
def adminUser = System.getenv("JENKINS_ADMIN_USER") ?: "admin"
def adminPass = System.getenv("JENKINS_ADMIN_PASS") ?: ""
def mcpUser  = System.getenv("JENKINS_MCP_USER") ?: "mcp-user"
def mcpPass  = System.getenv("JENKINS_MCP_PASS") ?: ""

// --- Create security realm ---
def hudsonRealm = new HudsonPrivateSecurityRealm(false)
hudsonRealm.createAccount(adminUser, adminPass)
hudsonRealm.createAccount(mcpUser, mcpPass)
instance.setSecurityRealm(hudsonRealm)

// --- Enable CSRF protection ---
instance.setCrumbIssuer(new hudson.security.csrf.DefaultCrumbIssuer(true))
instance.save()

// --- Ensure secrets folder exists ---
def secretsDir = Paths.get("/var/jenkins_home/secrets")
Files.createDirectories(secretsDir)

// --- Generate API token for MCP user ---
def mcp_user = hudson.model.User.get(mcpUser, true)
def apiProp = mcp_user.getProperty(ApiTokenProperty.class)
if (!apiProp) {
    apiProp = new ApiTokenProperty()
    mcp_user.addProperty(apiProp)
}
def tokenStore = apiProp.tokenStore
def newToken = tokenStore.generateNewToken("mcp-bootstrap")
def tokenValue = newToken.plainValue

// --- Save token to file ---
def tokenFile = secretsDir.resolve("mcp-user.token").toFile()
tokenFile.text = tokenValue
mcp_user.save()

println("MCP user: ${mcpUser}")
println("MCP token saved to: ${tokenFile.toString()}")
