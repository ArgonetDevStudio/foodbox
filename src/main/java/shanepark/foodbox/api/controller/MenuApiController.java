package shanepark.foodbox.api.controller;

import lombok.RequiredArgsConstructor;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;
import org.springframework.web.multipart.MultipartFile;
import shanepark.foodbox.api.domain.ApiResponse;
import shanepark.foodbox.api.domain.MenuResponse;
import shanepark.foodbox.api.service.MenuService;

import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.time.LocalDate;
import java.util.List;

@RestController
@RequestMapping("/api")
@RequiredArgsConstructor
public class MenuApiController {

    private final MenuService menuService;

    @GetMapping("/menu/today")
    public ApiResponse getTodayMenu() {
        MenuResponse menu = menuService.getTodayMenu(LocalDate.now());
        return ApiResponse.success(menu);
    }

    @GetMapping(value = "/menu")
    public ApiResponse getMenu() {
        return ApiResponse.success(menuService.findAll());
    }

    @GetMapping(value = "/crawl")
    public String crawl() {
        menuService.crawl();
        return "ok";
    }

    @PostMapping("upload")
    public ApiResponse upload(MultipartFile file) throws IOException {
        Path path = Files.createTempFile("temp", file.getOriginalFilename());
        file.transferTo(path);
        List<MenuResponse> parsedMenus = menuService.parseAndSave(path)
                .stream()
                .map(MenuResponse::of)
                .toList();

        return ApiResponse.success(parsedMenus);
    }

}
