package shanepark.foodbox.crawl;

import lombok.Getter;
import lombok.RequiredArgsConstructor;
import lombok.ToString;
import org.springframework.boot.context.properties.ConfigurationProperties;

@ConfigurationProperties(prefix = "crawl")
@RequiredArgsConstructor
@Getter
@ToString
public class CrawlConfig {
    private final String crawlUrl;
    private final String cssSelector;
    private final String crawlImageExpr;
    private final int imageIndex;
}
