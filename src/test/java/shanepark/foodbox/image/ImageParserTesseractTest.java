package shanepark.foodbox.image;

import org.junit.jupiter.api.Disabled;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.core.io.ClassPathResource;
import shanepark.foodbox.image.domain.ParsedMenu;
import shanepark.foodbox.image.ocr.ImageMarginCalculator;
import shanepark.foodbox.image.ocr.tesseract.ImageParserTesseract;

import java.io.IOException;
import java.util.List;

import static org.assertj.core.api.Assertions.assertThat;

@Disabled // Tesseract OCR tests take a long time to run
class ImageParserTesseractTest {
    private final Logger logger = LoggerFactory.getLogger(ImageParserTesseractTest.class);
    ImageParserTesseract parser = new ImageParserTesseract(new ImageMarginCalculator());

    @Test
    @DisplayName("Parse image and return parsed menu")
    void parse() throws IOException {
        ClassPathResource resource = new ClassPathResource("menu/menu-11112024.png");
        List<ParsedMenu> parsedMenu = parser.parse(resource.getFile().toPath());
        logger.info("parsedMenu: \n{}", parsedMenu);

        assertThat(parsedMenu).hasSize(10);
        assertThat(parsedMenu.getFirst().getDate()).matches("\\d{1,2}/\\d{1,2}\\([일월화수목금토]\\)");
    }

    @Test
    @DisplayName("OCT 28 image parse test")
    void parse2() throws IOException {
        ClassPathResource resource = new ClassPathResource("menu/menu-10282024.png");
        List<ParsedMenu> parsedMenu = parser.parse(resource.getFile().toPath());
        logger.info("parsedMenu: \n{}", parsedMenu);

        assertThat(parsedMenu).hasSize(10);
        assertThat(parsedMenu.getFirst().getDate()).matches("\\d{1,2}/\\d{1,2}\\([일월화수목금토]\\)");
    }

    @Test
    @DisplayName("DEC 09 image parse test")
    void parse3() throws IOException {
        ClassPathResource resource = new ClassPathResource("menu/menu-12092024-yuseong.png");
        List<ParsedMenu> parsedMenu = parser.parse(resource.getFile().toPath());
        logger.info("parsedMenu: \n{}", parsedMenu);

        assertThat(parsedMenu).hasSize(10);
        assertThat(parsedMenu.getFirst().getDate()).matches("\\d{1,2}/\\d{1,2}\\([일월화수목금토]\\)");
    }

    @Test
    @DisplayName("DEC 09 official website image parse test")
    void parse4() throws IOException {
        ClassPathResource resource = new ClassPathResource("menu/menu-12092024-official.png");
        List<ParsedMenu> parsedMenu = parser.parse(resource.getFile().toPath());
        logger.info("parsedMenu: \n{}", parsedMenu);

        assertThat(parsedMenu).hasSize(10);
        assertThat(parsedMenu.getFirst().getDate()).matches("\\d{1,2}/\\d{1,2}\\([일월화수목금토]\\)");
    }

    @Test
    @DisplayName("JAN 20 2025 image parse test")
    void parse5() throws IOException {
        ClassPathResource resource = new ClassPathResource("menu/menu-01202025.png");
        List<ParsedMenu> parsedMenu = parser.parse(resource.getFile().toPath());
        logger.info("parsedMenu: \n{}", parsedMenu);

        assertThat(parsedMenu).hasSize(10);
        assertThat(parsedMenu.getFirst().getDate()).matches("\\d{1,2}/\\d{1,2}\\([일월화수목금토]\\)");
    }

}
