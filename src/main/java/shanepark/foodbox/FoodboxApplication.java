package shanepark.foodbox;

import jakarta.annotation.PostConstruct;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.boot.context.properties.ConfigurationPropertiesScan;
import org.springframework.scheduling.annotation.EnableScheduling;
import shanepark.foodbox.crawl.CrawlConfig;
import shanepark.foodbox.slack.SlackConfig;

import java.time.LocalDateTime;

@SpringBootApplication
@ConfigurationPropertiesScan
@RequiredArgsConstructor
@EnableScheduling
@Slf4j
public class FoodboxApplication {

    private final CrawlConfig crawlConfig;
    private final SlackConfig slackConfig;

    @PostConstruct
    public void init() {
        log.info("=========================================");

        String arch = System.getProperty("os.arch");
        String os = System.getProperty("os.name");
        log.info("OS: {}, arch: {}", os, arch);

        log.info("CrawlConfig: {}", crawlConfig);
        log.info("SlackConfig: {}", slackConfig);
        log.info("Time zone: {}", System.getProperty("user.timezone"));
        log.info("Current time: {}", LocalDateTime.now());

        log.info("=========================================");
    }

    public static void main(String[] args) {
        SpringApplication.run(FoodboxApplication.class, args);
    }

}
