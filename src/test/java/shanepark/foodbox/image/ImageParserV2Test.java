package shanepark.foodbox.image;

import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.core.io.ClassPathResource;
import shanepark.foodbox.image.domain.ParsedMenu;
import shanepark.foodbox.image.service.ImageMarginCalculator;
import shanepark.foodbox.image.service.ImageParser;
import shanepark.foodbox.image.service.ImageParserV2;
import shanepark.foodbox.ocr.OCRConfig;

import java.io.IOException;
import java.io.InputStream;
import java.util.List;

import static org.assertj.core.api.Assertions.assertThat;


class ImageParserV2Test {
    private final Logger logger = LoggerFactory.getLogger(ImageParserV2Test.class);
    ImageParserV2 parser = new ImageParserV2();

    @Test
    @DisplayName("Parse image and return parsed menu")
    void parse() throws IOException {
        OCRConfig config = new OCRConfig("","");
        ClassPathResource resource = new ClassPathResource("menu-nov-11.png");
        try (InputStream ins = resource.getInputStream()) {
            parser.parse(ins,config);
        }catch (Exception e){
            e.printStackTrace();
        }
    }

}
