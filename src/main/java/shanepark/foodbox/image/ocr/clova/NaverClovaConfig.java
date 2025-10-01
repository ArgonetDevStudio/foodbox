package shanepark.foodbox.image.ocr.clova;

import lombok.Getter;
import lombok.RequiredArgsConstructor;
import lombok.ToString;
import org.springframework.boot.context.properties.ConfigurationProperties;

@ConfigurationProperties(prefix = "clova")
@Getter
@ToString
@RequiredArgsConstructor
public class NaverClovaConfig {
    private final String url;
    private final String secretKey;

}
