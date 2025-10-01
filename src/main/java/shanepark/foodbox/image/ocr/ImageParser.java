package shanepark.foodbox.image.ocr;

import shanepark.foodbox.image.domain.ParsedMenu;

import java.io.IOException;
import java.nio.file.Path;
import java.util.List;

public interface ImageParser {
    List<ParsedMenu> parse(Path path) throws IOException;
}
