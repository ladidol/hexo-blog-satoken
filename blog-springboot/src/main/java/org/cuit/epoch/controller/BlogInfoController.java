package org.cuit.epoch.controller;

import cn.dev33.satoken.annotation.SaCheckLogin;
import cn.dev33.satoken.annotation.SaCheckRole;
import io.swagger.annotations.Api;
import io.swagger.annotations.ApiOperation;
import org.cuit.epoch.dto.BlogHomeInfoDTO;
import org.cuit.epoch.service.BlogInfoService;
import org.cuit.epoch.util.Result;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PutMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RestController;

import javax.validation.Valid;

/**
 * @author: ladidol
 * @date: 2022/11/16 19:07
 * @description:
 */
@Api(tags = "博客信息模块")
@RestController
public class BlogInfoController {
    @Autowired
    private BlogInfoService blogInfoService;
//    @Autowired
//    private WebSocketServiceImpl webSocketService;
//    @Autowired
//    private UploadStrategyContext uploadStrategyContext;

    /**
     * 查看博客信息
     *
     * @return {@link Result<BlogHomeInfoDTO>} 博客信息
     */
    @ApiOperation(value = "查看博客网站主页基本信息")
    @GetMapping("/")
    public Result<BlogHomeInfoDTO> getBlogHomeInfo() {
        return Result.ok(blogInfoService.getBlogHomeInfo());
    }

//    /**
//     * 查看后台信息
//     *
//     * @return {@link Result<BlogBackInfoDTO>} 后台信息
//     */
//    @ApiOperation(value = "查看后台信息")
//    @GetMapping("/admin")
//    public Result<BlogBackInfoDTO> getBlogBackInfo() {
//        return Result.ok(blogInfoService.getBlogBackInfo());
//    }
//
//    /**
//     * 上传博客配置图片
//     *
//     * @param file 文件
//     * @return {@link Result<String>} 博客配置图片
//     */
//    @ApiOperation(value = "上传博客配置图片")
//    @ApiImplicitParam(name = "file", value = "图片", required = true, dataType = "MultipartFile")
//    @PostMapping("/admin/config/images")
//    public Result<String> savePhotoAlbumCover(MultipartFile file) {
//        return Result.ok(uploadStrategyContext.executeUploadStrategy(file, FilePathEnum.CONFIG.getPath()));
//    }
//
//    /**
//     * 更新网站配置
//     *
//     * @param websiteConfigVO 网站配置信息
//     * @return {@link Result}
//     */
//    @ApiOperation(value = "更新网站配置")
//    @PutMapping("/admin/website/config")
//    public Result<?> updateWebsiteConfig(@Valid @RequestBody WebsiteConfigVO websiteConfigVO) {
//        blogInfoService.updateWebsiteConfig(websiteConfigVO);
//        return Result.ok();
//    }
//
//    /**
//     * 获取网站配置
//     *
//     * @return {@link Result<WebsiteConfigVO>} 网站配置
//     */
//    @ApiOperation(value = "获取网站配置")
//    @GetMapping("/admin/website/config")
//    public Result<WebsiteConfigVO> getWebsiteConfig() {
//        return Result.ok(blogInfoService.getWebsiteConfig());
//    }
//
//    /**
//     * 查看关于我信息
//     *
//     * @return {@link Result<String>} 关于我信息
//     */
//    @ApiOperation(value = "查看关于我信息")
//    @GetMapping("/about")
//    public Result<String> getAbout() {
//        return Result.ok(blogInfoService.getAbout());
//    }
//
//    /**
//     * 修改关于我信息
//     *
//     * @param blogInfoVO 博客信息
//     * @return {@link Result<>}
//     */
//    @OptLog(optType = UPDATE)
//    @ApiOperation(value = "修改关于我信息")
//    @PutMapping("/admin/about")
//    public Result<?> updateAbout(@Valid @RequestBody BlogInfoVO blogInfoVO) {
//        blogInfoService.updateAbout(blogInfoVO);
//        return Result.ok();
//    }
//
//    /**
//     * 保存语音信息
//     *
//     * @param voiceVO 语音信息
//     * @return {@link Result<String>} 语音地址
//     */
//    @ApiOperation(value = "上传语音")
//    @PostMapping("/voice")
//    public Result<String> sendVoice(VoiceVO voiceVO) {
//        webSocketService.sendVoice(voiceVO);
//        return Result.ok();
//    }
//
//    /**
//     * 上传访客信息
//     *
//     * @return {@link Result}
//     */
//    @PostMapping("/report")
//    public Result<?> report() {
//        blogInfoService.report();
//        return Result.ok();
//    }

}