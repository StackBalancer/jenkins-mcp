import jenkins.model.*
import hudson.model.*

println("--> Creating seed job")

def instance = Jenkins.getInstance()
def jobName = "demo-job"

if (instance.getItem(jobName) == null) {
    def job = new FreeStyleProject(instance, jobName)
    job.setDescription("A demo job to showcase MCP integration.")
    job.buildersList.add(new hudson.tasks.Shell(
        "echo 'Hello from Jenkins MCP!' && sleep 2 && echo 'Build complete.'"
    ))
    instance.add(job, jobName)
    println("--> Seed job '${jobName}' created")
} else {
    println("--> Seed job '${jobName}' already exists")
}
