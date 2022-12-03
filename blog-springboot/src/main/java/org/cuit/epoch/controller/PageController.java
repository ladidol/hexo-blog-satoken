package org.cuit.epoch.controller;

import io.swagger.annotations.Api;
import io.swagger.annotations.ApiImplicitParam;
import io.swagger.annotations.ApiOperation;
import org.cuit.epoch.annotation.OptLog;
import org.cuit.epoch.service.PageService;
import org.cuit.epoch.util.Result;
import org.cuit.epoch.vo.PageVO;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.web.bind.annotation.*;

import javax.validation.Valid;
import java.util.List;

import static org.cuit.epoch.enums.OptTypeConst.REMOVE;
import static org.cuit.epoch.enums.OptTypeConst.SAVE_OR_UPDATE;

/**
 * @author: ladidol
 * @date: 2022/12/3 17:52
 * @description:
 */
@Api(tags = "页面模块")
@RestController
public class PageController {
    @Autowired
    private PageService pageService;

    /**
     * 删除页面
     *
     * @param pageId 页面id
     * @return {@link Result <>}
     */
    @OptLog(optType = REMOVE)
    @ApiOperation(value = "删除页面")
    @ApiImplicitParam(name = "pageId", value = "页面id", required = true, dataType = "Integer")
    @DeleteMapping("/admin/pages/{pageId}")
    public Result<?> deletePage(@PathVariable("pageId") Integer pageId) {
        pageService.deletePage(pageId);
        return Result.ok();
    }

    /**
     * 保存或更新页面
     *
     * @param pageVO 页面信息
     * @return {@link Result<>}
     */
    @OptLog(optType = SAVE_OR_UPDATE)
    @ApiOperation(value = "保存或更新页面")
    @PostMapping("/admin/pages")
    public Result<?> saveOrUpdatePage(@Valid @RequestBody PageVO pageVO) {
        pageService.saveOrUpdatePage(pageVO);
        return Result.ok();
    }

    /**
     * 获取页面列表
     *
     * @return {@link Result<PageVO>}
     */
    @ApiOperation(value = "获取页面列表")
    @GetMapping("/admin/pages")
    public Result<List<PageVO>> listPages() {
        return Result.ok(pageService.listPages());
    }

}
